package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/nats-io/go-nats-streaming"
	"github.com/nats-io/go-nats-streaming/pb"
	"github.com/relistan/envconfig"
	"github.com/Nitro/lagermeister/message"
	"github.com/gogo/protobuf/proto"
)

const (
	BatchSize = 25
)

func printMsg(m *stan.Msg, i int) {
	log.Printf("[#%d] Received on [%s]: '%s'\n", i, m.Subject, m)
}

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

	stanConn     stan.Conn
	subscription stan.Subscription
	batchedRecords chan *message.Message
	lastSent    time.Time
	pool        *bpool.BufferPool
}

func NewLogFollower() *LogFollower {
	return &LogFollower{
		batchedRecords: make(chan *message.Message, BatchSize),
		pool:
	}
}

func (f *LogFollower) Connect() error {
	var err error

	f.stanConn, err = stan.Connect(f.ClusterId, f.ClientId, stan.NatsURL(f.NatsUrl))
	if err != nil {
		return fmt.Errorf("Can't connect: %v.\nMake sure a NATS Streaming Server is running at: %s", err, f.NatsUrl)
	}
	log.Printf("Connected to %s clusterID: [%s] clientID: [%s]\n", f.NatsUrl, f.ClusterId, f.ClientId)

	return nil
}

// SendBatch takes what's currently in the batchedRecords channel and sends
// it to the remote service.
func (f *LogFollower) SendBatch() {
	data := make([]*message.Message, BatchSize)

	for i := 0; i < BatchSize; i++ {
		data[i] = <-f.batchedRecords
	}

	log.Warnf("Batched %d records", len(data))
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
	case f.batchedRecords <-&record: // do nothing
	default:
		go f.SendBatch()
		log.Debug("Sending batch, waiting to batch new record")
		f.batchedRecords <-&record
	}
}

// Follow begins following the Subject in the NATS streaming host
func (f *LogFollower) Follow() error {
	startOpt := stan.StartAt(pb.StartPosition_NewOnly)

	if f.StartSeq != 0 {
		startOpt = stan.StartAtSequence(f.StartSeq)
	} else if f.DeliverLast == true {
		startOpt = stan.StartWithLastReceived()
	} else if f.DeliverAll == true {
		log.Print("subscribing with DeliverAllAvailable")
		startOpt = stan.DeliverAllAvailable()
	} else if f.StartDelta != "" {
		ago, err := time.ParseDuration(f.StartDelta)
		if err != nil {
			f.stanConn.Close()
			log.Fatal(err)
		}
		startOpt = stan.StartAtTimeDelta(ago)
	}

	var i int
	mcb := func(msg *stan.Msg) {
		i++
		printMsg(msg, i)
		f.BatchRecord(msg)
	}

	var err error
	f.subscription, err = f.stanConn.QueueSubscribe(
		f.Subject, f.Qgroup, mcb, startOpt, stan.DurableName(f.Durable),
	)
	if err != nil {
		f.stanConn.Close()
		return fmt.Errorf("Unable to subscribe: %s", err)
	}

	return nil
}

// Unfollow stops following the logs
func (f *LogFollower) Unfollow() {
	f.subscription.Unsubscribe()
}

// Shutdown disconnects from the NATS streaming server
func (f *LogFollower) Shutdown() {
	f.stanConn.Close()
}

func main() {
	log.SetLevel(log.DebugLevel)

	var follower LogFollower
	envconfig.Process("sub", &follower)
	follower.batchedRecords = make(chan *message.Message, BatchSize)

	fmt.Printf("%#v\n", follower)
	if follower.ClientId == "" {
		log.Printf("Error: A unique client ID must be specified.")
		os.Exit(1)
	}
	if follower.Subject == "" {
		log.Printf("Error: A subject must be specified.")
		os.Exit(1)
	}

	err := follower.Connect()
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
	fmt.Printf("\nReceived an interrupt, unsubscribing and closing connection...\n\n")
	// Do not unsubscribe a durable on exit, except if asked to.
	if follower.Durable == "" || follower.Unsubscribe {
		follower.Unfollow()
	}
	follower.Shutdown()
}
