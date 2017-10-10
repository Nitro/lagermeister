package main

import (
	_ "expvar"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/Nitro/lagermeister/event"
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
	BatchSize          = 100
	MaxEncodedSize     = 21 * 1024 // Maximum encoded JSON size
	OutRecordSize      = BatchSize*MaxEncodedSize + BatchSize + 10
	CheckTime          = 5 * time.Second  // Check the NATS connection
	PosterPoolSize     = 25               // HTTP posters
	DefaultHttpTimeout = 10 * time.Second // When posting to e.g. Sumologic
)

type MessageHandler interface {
	HandleMessage(m *stan.Msg, i int) error
}

type PrintMessageHandler struct{}

func (p *PrintMessageHandler) HandleMessage(m *stan.Msg, i int) error {
	log.Debugf("[#%d] Received on [%s]: '%s'\n", i, m.Subject, m)
	return nil
}

var (
	printer *PrintMessageHandler
)

// LogFollower attaches to a subject on NATS streaming and processes new
// log messages as they are received.
type LogFollower struct {
	ClusterId    string `envconfig:"CLUSTER_ID" default:"test-cluster"`
	ClientId     string `envconfig:"CLIENT_ID"`
	StartSeq     uint64 `envconfig:"START_SEQ"`
	StartDelta   string `envconfig:"START_DELTA"`
	DeliverAll   bool   `envconfig:"DELIVER_ALL"`
	DeliverLast  bool   `envconfig:"DELIVER_LAST"`
	Durable      string `envconfig:"DURABLE"`
	Qgroup       string `envconfig:"QGROUP"`
	Unsubscribe  bool   `envconfig:"UNSUBSCRIBE"`
	NatsUrl      string `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	Subject      string `envconfig:"SUBJECT"`
	StubHttp     bool   `envconfig:"STUB_HTTP" default:"false"`
	RemoteUrl    string `envconfig:"REMOTE_URL" required:"true"`
	LoggingLevel string `envconfig:"LOGGING_LEVEL" default:"info"`

	stanConn       stan.Conn
	subscription   stan.Subscription
	batchedRecords chan *message.Message
	lastSent       time.Time
	outPool        *bpool.BytePool
	quitChan       chan struct{}
	startOpt       stan.SubscriptionOption
	poster         *HttpMessagePoster
	lastSeenTime   time.Time // The timestamp from the last event we saw
	lastWallTime   time.Time // The last actual time we saw an event
	MetricReporter *event.MetricReporter
}

func NewLogFollower() *LogFollower {
	return &LogFollower{
		batchedRecords: make(chan *message.Message, BatchSize),
		outPool:        bpool.NewBytePool(2, OutRecordSize),
		quitChan:       make(chan struct{}),
		lastSeenTime:   time.Unix(0, 0),
		lastWallTime:   time.Unix(0, 0),
	}
}

// Connect initates a connection to the NATS streaming server.
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
			if f.stanConn == nil || f.stanConn.NatsConn() == nil || !f.stanConn.NatsConn().IsConnected() {
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

	// If there's nothing batched, bail now
	if len(f.batchedRecords) < 1 {
		return
	}

	// TODO this can block for an indefinite amount of time without sending
	// messages if we don't receive any more...
	var rec *message.Message
	for i := 0; i < BatchSize; i++ {
		rec = <-f.batchedRecords

		buf, err := ffjson.Marshal(rec)
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

		// Track the last actual time we saw an event (for lag calculation)
		now := time.Now().UTC()
		if f.lastWallTime.Before(now) {
			f.lastWallTime = now
		}

		// Kinda dangerous to do this in a for loop, let's see how it goes
		defer ffjson.Pool(buf)
	}

	// Track the last timestamp we saw (for lag calculation)
	recTime := time.Unix(*rec.Timestamp/1e9, 0)
	if f.lastSeenTime.Before(recTime) {
		f.lastSeenTime = recTime
	}

	f.send(data[:startRec])
}

// reportLag runs in the background and reports a lag metric to the stats
// system.
func (f *LogFollower) reportLag() {
	for {
		select {
		case <-time.After(1 * time.Second):
			f.sendLagMetric()
		case <-f.quitChan:
			return
		}
	}
}

func (f *LogFollower) sendLagMetric() {
	lag := f.lastWallTime.Sub(f.lastSeenTime)
	if lag < 0 {
		log.Warn("Stale lag metric... timestamps are not in sync!")
		lag = 0
	}

	if f.MetricReporter != nil {
		f.MetricReporter.TrySendMetrics(&event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			Value:      lag.Seconds(),
			Sender:     "http-output", // TODO make this configurable
			MetricType: "Lag",
			Aggregate:  "Total",
			Threshold: map[string]float64{
				"Warn": 30,
				"Error": 60,
			},
		})
	}
}

func (f *LogFollower) send(buf []byte) {
	f.poster.Post(buf)
	// TODO remove this when debugging is complete!
	// ioutil.WriteFile("out.json", buf, 0644)
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

	go f.reportLag()

	return nil
}

// Unfollow stops following the logs
func (f *LogFollower) Unfollow() {
	if f.subscription != nil {
		f.subscription.Unsubscribe()
	}
}

// Shutdown disconnects from the NATS streaming server
func (f *LogFollower) Shutdown() {
	f.quitChan <- struct{}{}
	if f.stanConn != nil {
		f.stanConn.Close()
	}
}

func serveHttp() {
	http.ListenAndServe(":34999", nil)
}

func configureLoggingLevel(level string) {
	switch {
	case len(level) == 0:
		log.SetLevel(log.InfoLevel)
	case level == "info":
		log.SetLevel(log.InfoLevel)
	case level == "warn":
		log.SetLevel(log.WarnLevel)
	case level == "error":
		log.SetLevel(log.ErrorLevel)
	case level == "debug":
		log.SetLevel(log.DebugLevel)
	}
}

func main() {
	log.SetLevel(log.InfoLevel)

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
	configureLoggingLevel(follower.LoggingLevel)

	if follower.StubHttp {
		follower.poster = NewStubHttpMessagePoster(follower.RemoteUrl, DefaultHttpTimeout)
	} else {
		follower.poster = NewHttpMessagePoster(follower.RemoteUrl, DefaultHttpTimeout)
	}

	// Don't let people confuse this for a boolean option
	if follower.Durable == "false" || follower.Durable == "true" {
		log.Fatalf("Invalid durable queue name: %s. This is not a boolean option.", follower.Durable)
	}

	if follower.ClientId == "" {
		log.Fatalf("Error: A unique client ID must be specified.")
	}
	if follower.Subject == "" {
		log.Fatalf("Error: A subject must be specified.")
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

	reporter := event.NewMetricReporter()
	err = reporter.ProcessMetrics()
	if err != nil {
		log.Fatalf("Unable to connect to NATS for stats reporting! (%s)", err)
	}
	follower.MetricReporter = reporter
	follower.poster.MetricReporter = reporter

	// Wait for a SIGINT (perhaps triggered by user with CTRL-C)
	// Run cleanup when signal is received
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	go serveHttp()

	<-signalChan
	log.Warnf("\nReceived an interrupt, unsubscribing and closing connection...\n\n")
	// Do not unsubscribe a durable on exit, except if asked to.
	if follower.Durable == "" || follower.Unsubscribe {
		follower.Unfollow()
	}
	follower.Shutdown()
}
