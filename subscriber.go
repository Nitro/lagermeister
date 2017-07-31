package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	"github.com/gogo/protobuf/proto"
	"github.com/nats-io/go-nats-streaming"
	"github.com/nats-io/go-nats-streaming/pb"
	"github.com/oxtoacart/bpool"
	"github.com/pquerna/ffjson/ffjson"
	"github.com/relistan/envconfig"
	"gopkg.in/relistan/rubberneck.v1"
)

const (
	BatchSize          = 25
	MaxEncodedSize     = 21 * 1024 // Maximum encoded JSON size
	OutRecordSize      = BatchSize*MaxEncodedSize + BatchSize + 10
	CheckTime          = 5 * time.Second  // Check the NATS connection
	PosterPoolSize     = 10               // HTTP posters
	DefaultHttpTimeout = 10 * time.Second // When posting to e.g. Sumologic
)

type MessageHandler interface {
	HandleMessage(m *stan.Msg, i int) error
}

type PrintMessageHandler struct{}

func (p *PrintMessageHandler) HandleMessage(m *stan.Msg, i int) error {
	log.Printf("[#%d] Received on [%s]: '%s'\n", i, m.Subject, m)
	return nil
}

var (
	printer *PrintMessageHandler
)

type LogFollower struct {
	ClusterId   string `envconfig:"CLUSTER_ID" default:"test-cluster"`
	ClientId    string `envconfig:"CLIENT_ID"`
	StartSeq    uint64 `envconfig:"START_SEQ"`
	StartDelta  string `envconfig:"START_DELTA"`
	DeliverAll  bool   `envconfig:"DELIVER_ALL"`
	DeliverLast bool   `envconfig:"DELIVER_LAST"`
	Durable     string `envconfig:"DURABLE"`
	Qgroup      string `envconfig:"QGROUP"`
	Unsubscribe bool   `envconfig:"UNSUBSCRIBE"`
	NatsUrl     string `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	Subject     string `envconfig:"SUBJECT"`
	RemoteUrl   string `envconfig:"REMOTE_URL" required:"true"`

	stanConn       stan.Conn
	subscription   stan.Subscription
	batchedRecords chan *message.Message
	lastSent       time.Time
	outPool        *bpool.BytePool
	quitChan       chan struct{}
	startOpt       stan.SubscriptionOption
	poster         *HttpMessagePoster
}

func NewLogFollower() *LogFollower {
	return &LogFollower{
		batchedRecords: make(chan *message.Message, BatchSize),
		outPool:        bpool.NewBytePool(2, OutRecordSize),
		quitChan:       make(chan struct{}),
	}
}

func (f *LogFollower) Connect() error {
	var err error

	f.stanConn, err = stan.Connect(f.ClusterId, f.ClientId, stan.NatsURL(f.NatsUrl))
	if err != nil {
		return fmt.Errorf(
			"Can't connect: %v.\nMake sure a NATS Streaming Server is running at: %s",
			err, f.NatsUrl,
		)
	}
	log.Printf("Connected to %s clusterID: [%s] clientID: [%s]\n",
		f.NatsUrl, f.ClusterId, f.ClientId,
	)

	return nil
}

// manageConnection keeps the stan/NATS connection alive
func (f *LogFollower) manageConnection() {
	for {
		select {
		case <-time.After(5 * time.Second):
			if f.stanConn == nil || !f.stanConn.NatsConn().IsConnected() {
				log.Warn("Disconnected... reconnecting")
				f.Connect()
				f.subscribe()
			}
		case <-f.quitChan:
			return
		}
	}
}

// SendBatch takes what's currently in the batchedRecords channel and sends
// it to the remote service.
func (f *LogFollower) SendBatch() {
	var startRec int

	data := f.outPool.Get()
	defer f.outPool.Put(data)

	for i := 0; i < BatchSize; i++ {
		buf, err := ffjson.Marshal(<-f.batchedRecords)
		if err != nil {
			log.Errorf("Error encoding record: %s", err)
			continue
		}

		if startRec+len(buf) > len(data) {
			log.Warnf("Batch buffer size exceeded!")
			f.send(data[:startRec])
			return
		}

		copy(data[startRec:], buf)
		startRec += len(buf)
		data[startRec] = '\n'
		startRec += 1

		// Kinda dangerous to do this in a for loop, let's see how it goes
		defer ffjson.Pool(buf)
	}

	f.send(data[:startRec])
}

func (f *LogFollower) send(buf []byte) {
	f.poster.Post(buf)
	// TODO remove this when debugging is complete!
	ioutil.WriteFile("out.json", buf, 0644)
}

// BatchRecord adds a record to the batch channel after Unmarshaling it from
// Protobuf encoding.
func (f *LogFollower) BatchRecord(msg *stan.Msg) {
	var record message.Message
	err := proto.Unmarshal(msg.Data, &record)
	if err != nil {
		log.Errorf("Unable to unmarshal record, dropping: %s", err)
		return
	}

	select {
	case f.batchedRecords <- &record: // do nothing
	default:
		go f.SendBatch()
		log.Debug("Sending batch, waiting to batch new record")
		f.batchedRecords <- &record
	}
}

func (f *LogFollower) subscribe() error {
	if f.stanConn == nil {
		return fmt.Errorf("Can't subscribe, stan connection was nil!")
	}
	var i int
	mcb := func(msg *stan.Msg) {
		i++
		printer.HandleMessage(msg, i)
		f.BatchRecord(msg)
	}

	var err error
	f.subscription, err = f.stanConn.QueueSubscribe(
		f.Subject, f.Qgroup, mcb, f.startOpt, stan.DurableName(f.Durable),
	)
	if err != nil {
		f.stanConn.Close()
		fmt.Errorf("Unable to subscribe: %s", err)
	}

	return nil
}

// Follow begins following the Subject in the NATS streaming host
func (f *LogFollower) Follow() error {
	f.startOpt = stan.StartAt(pb.StartPosition_NewOnly)

	if f.StartSeq != 0 {
		f.startOpt = stan.StartAtSequence(f.StartSeq)
	} else if f.DeliverLast == true {
		log.Info("Subscribing to only most recent")
		f.startOpt = stan.StartWithLastReceived()
	} else if f.DeliverAll == true {
		log.Print("subscribing with DeliverAllAvailable")
		f.startOpt = stan.DeliverAllAvailable()
	} else if f.StartDelta != "" {
		ago, err := time.ParseDuration(f.StartDelta)
		if err != nil {
			f.stanConn.Close()
			log.Fatal(err)
		}
		f.startOpt = stan.StartAtTimeDelta(ago)
	}

	err := f.subscribe()
	if err != nil {
		log.Error(err)
	}

	go f.manageConnection()

	return nil
}

// Unfollow stops following the logs
func (f *LogFollower) Unfollow() {
	f.subscription.Unsubscribe()
}

// Shutdown disconnects from the NATS streaming server
func (f *LogFollower) Shutdown() {
	f.quitChan <- struct{}{}
	f.stanConn.Close()
}

type HttpMessagePoster struct {
	Url     string
	Timeout time.Duration

	client *http.Client
	pool   chan struct{}
}

func NewHttpMessagePoster(url string, timeout time.Duration) *HttpMessagePoster {
	poster := &HttpMessagePoster{
		Url:     url,
		Timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
		pool: make(chan struct{}, PosterPoolSize),
	}

	for i := 0; i < PosterPoolSize; i++ {
		poster.pool <- struct{}{}
	}

	return poster
}

// Post is potentially blocking since it checks out a semaphore from
// the pool. If the pool is maxxed out, the call will block until a
// semaphore is returned to the pool.
func (h *HttpMessagePoster) Post(data []byte) {
	if h.client == nil {
		log.Error("Client has not been initialized!")
		return
	}

	// Post requires a Reader so we make one from the byte slice
	buf := bytes.NewBuffer(data)

	// Check out a semaphore so we don't have too many at once
	log.Debugf("Free semaphores: %d/%d", len(h.pool), PosterPoolSize)
	semaphore := <-h.pool
	if len(h.pool) < 1 {
		log.Warn("All semaphores in use!")
	}

	go func() {
		startTime := time.Now()
		defer func() {
			log.Debugf("POST time taken: %s", time.Now().Sub(startTime))
		}()

		resp, err := h.client.Post(h.Url, "application/json", buf)
		h.pool <- semaphore // This should not block if implemented properly

		log.Debugf("Response code: %d", resp.StatusCode)

		// TODO read this and log response on error
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}

		if err != nil || resp.StatusCode < 200 || resp.StatusCode > 299 {
			log.Errorf(
				"Error posting to %s. Status %d. Error: %s", h.Url, resp.StatusCode, err,
			)
		}
	}()
}

func main() {
	log.SetLevel(log.DebugLevel)

	printer = &PrintMessageHandler{}

	var follower LogFollower
	err := envconfig.Process("sub", &follower)
	if err != nil {
		log.Errorf("Processing config: %s", err)
		os.Exit(1)
	}
	rubberneck.Print(follower)

	follower.batchedRecords = make(chan *message.Message, BatchSize)
	follower.outPool = bpool.NewBytePool(2, OutRecordSize)
	follower.quitChan = make(chan struct{})
	follower.poster = NewHttpMessagePoster(follower.RemoteUrl, DefaultHttpTimeout)

	// Don't let people confuse this for a boolean option
	if follower.Durable == "false" || follower.Durable == "true" {
		log.Errorf("Invalid durable queue name: %s. This is not a boolean option.", follower.Durable)
		os.Exit(1)
	}

	if follower.ClientId == "" {
		log.Printf("Error: A unique client ID must be specified.")
		os.Exit(1)
	}
	if follower.Subject == "" {
		log.Printf("Error: A subject must be specified.")
		os.Exit(1)
	}

	err = follower.Connect()
	if err != nil {
		log.Fatal(err)
	}

	err = follower.Follow()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Listening on [%s], clientID=[%s], qgroup=[%s] durable=[%s]\n",
		follower.Subject, follower.ClientId, follower.Qgroup, follower.Durable,
	)

	// Wait for a SIGINT (perhaps triggered by user with CTRL-C)
	// Run cleanup when signal is received
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	<-signalChan
	log.Warnf("\nReceived an interrupt, unsubscribing and closing connection...\n\n")
	// Do not unsubscribe a durable on exit, except if asked to.
	if follower.Durable == "" || follower.Unsubscribe {
		follower.Unfollow()
	}
	follower.Shutdown()
}
