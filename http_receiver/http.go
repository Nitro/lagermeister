package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	"github.com/gogo/protobuf/proto"
	"github.com/nats-io/go-nats-streaming"
	"github.com/oxtoacart/bpool"
)

const (
	DefaultPoolSize       = 100
	DefaultPoolMemberSize = 21 * 1024
)

var (
	publishRetries = [...]int{250, 500, 1000, 3000, 5000} // Milliseconds
	ProcessedCount int64
	StatsMutex     sync.RWMutex
)

type HttpRelay struct {
	Address   string
	NatsUrl   string
	ClusterId string
	ClientId  string
	Subject   string
	MatchSpec string // Heka message matcher

	stanConn stan.Conn
	matcher  *message.MatcherSpecification
	pool     *bpool.BytePool
}

// Relay sets up the HTTP listener and the NATS client, then starts
// handling traffic between the HTTP input and NATS output.
func (h *HttpRelay) Relay() error {
	var err error

	if len(h.MatchSpec) > 0 {
		h.matcher, err = message.CreateMatcherSpecification(h.MatchSpec)
		if err != nil {
			return fmt.Errorf("Unable to parse matcher spec: %s", err)
		}
	}

	h.pool = bpool.NewBytePool(DefaultPoolSize, DefaultPoolMemberSize)

	err = h.connectStan()
	if err != nil {
		return err // Already annotated in the connectStan function
	}

	http.HandleFunc("/", h.handleReceive)
	err = http.ListenAndServe(h.Address, nil)
	if err != nil {
		return fmt.Errorf("HTTP listener: %s", err)
	}

	return nil
}

// connectStan connects to the NATS streaming cluster
func (h *HttpRelay) connectStan() error {
	var err error

	h.stanConn, err = stan.Connect(
		h.ClusterId, h.ClientId, stan.NatsURL(h.NatsUrl),
		stan.ConnectWait(1*time.Second),
		stan.PubAckWait(2*time.Second),
	)
	if err != nil {
		// A ton of failures seem to derive from the Cluster ID not matching on
		// connect. The error reported up from the stan package is not very
		// helpful.
		return fmt.Errorf("Connecting to NATS streaming: %s", err)
	}

	return nil
}

// relayMessage publishes a message to NATS streaming. It is blocking and can
// hold onto the goroutine for several seconds so it should be run only where
// that won't cause any performance issues.
func (h *HttpRelay) relayMessage(msg *message.Message) {
	data, err := msg.Marshal()
	if err != nil {
		log.Errorf("Encoding: %s", err)
	}

	for i, sleepTime := range publishRetries {
		if h.stanConn == nil {
			log.Warn("Reconnecting to NATS")
			err = h.connectStan()
			if err != nil {
				log.Warnf("Retrying #%d publishing to NATS", i)
				time.Sleep(time.Duration(sleepTime) * time.Millisecond)
				continue
			}
		}

		err = h.stanConn.Publish(h.Subject, data)
		if err == nil {
			break
		}

		if err == stan.ErrConnectionClosed || err == stan.ErrBadConnection ||
			err == stan.ErrTimeout {

			log.Warnf("Retrying #%d publishing to NATS", i)
			h.stanConn = nil

			continue
		}

		log.Errorf("Publishing: %s", err)
	}
}

// handlReceive is the HTTP endpoint that accepts log messages and send them on to
// the message broker for further processing.
func (h *HttpRelay) handleReceive(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	data, bytesRead, err := h.readAll(req.Body)

	StatsMutex.Lock()
	ProcessedCount += 1
	StatsMutex.Unlock()

	if ProcessedCount%1000 == 0 {
		log.Infof("Processed: %d", ProcessedCount)
	}

	if err != nil {
		log.Error(err)
	}

	var msg message.Message
	// Using a length limit is important here! Otherwise msg will include
	// all the null chars at the end of the byte slice.
	proto.Unmarshal(data[:bytesRead], &msg)

	defer h.pool.Put(data)

	if msg.Payload != nil {
		niceTime := time.Unix(0, *msg.Timestamp)
		if msg.Payload == nil || msg.Hostname == nil {
			log.Warn("Missing fields!")
			return
		}
		fmt.Printf("%s %s: %#v\n", niceTime, *msg.Hostname, *msg.Payload)
		if h.matcher == nil || h.matcher.Match(&msg) {
			h.relayMessage(&msg)
		}
	}
}

func (h *HttpRelay) readAll(r io.Reader) (b []byte, bytesRead int, err error) {
	buf := h.pool.Get()

	for {
		nBytes, err := r.Read(buf[bytesRead:])
		bytesRead += nBytes

		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, 0, err
		}
	}

	log.Debugf("Bytes Read: %d", bytesRead)

	if bytesRead == DefaultPoolMemberSize {
		log.Warnf("Possible message overflow, %d bytes read", bytesRead)
	}

	return buf, bytesRead, nil
}

func main() {
	relay := &HttpRelay{
		Address:   ":35001",
		NatsUrl:   "nats://localhost:4222",
		ClientId:  "lagermeister",
		ClusterId: "test-cluster",
		Subject:   "lagermeister-test",
		//MatchSpec: "Logger == 'hekad' && Hostname == 'ubuntu'",
	}

	err := relay.Relay()
	if err != nil {
		panic(err)
	}
}
