package main

import (
	"expvar"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	"github.com/gogo/protobuf/proto"
	"github.com/kelseyhightower/envconfig"
	"github.com/nats-io/go-nats-streaming"
	"github.com/oxtoacart/bpool"
	"gopkg.in/relistan/rubberneck.v1"
)

const (
	DefaultPoolSize       = 100
	DefaultPoolMemberSize = 21 * 1024
)

var (
	publishRetries = [...]int{250, 500, 1000, 3000, 5000} // Milliseconds

	stats = expvar.NewMap("stats")
)

type HttpRelay struct {
	Address   string `envconfig:"BIND_ADDRESS" default:":35001"`
	NatsUrl   string `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	ClusterId string `envconfig:"CLUSTER_ID" default:"test-cluster"`
	ClientId  string `envconfig:"CLIENT_ID" required:"true"`
	Subject   string `envconfig:"SUBJECT" default:"lagermeister-test"`
	MatchSpec string `envconfig:"MATCH_SPEC"` // Heka message matcher

	stanConn  stan.Conn
	matcher   *message.MatcherSpecification
	pool      *bpool.BytePool
	available bool
	availLock sync.RWMutex
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

	h.breakerOff()

	return nil
}

// breakerOn flips the circuit breaker to on, so that we don't accept any
// messages that we won't be able to store. We could end up dropping the
// first message that has an issue because we don't actively manage the NATS
// connection.
func (h *HttpRelay) breakerOn() {
	h.availLock.Lock()
	h.available = false
	log.Warn("Turning circuit breaker on!")
	h.availLock.Unlock()
}

// breakerOff flips the circuit breaker to off so that we can process any new
// incoming messages.
func (h *HttpRelay) breakerOff() {
	h.availLock.Lock()
	h.available = true
	log.Warn("Turning circuit breaker off!")
	h.availLock.Unlock()
}

func (h *HttpRelay) isAvailable() bool {
	h.availLock.Lock()
	defer h.availLock.Unlock()

	return h.available
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
			stats.Add("publishedCount", 1)
			break
		}

		h.breakerOn()

		if err == stan.ErrConnectionClosed || err == stan.ErrBadConnection ||
			err == stan.ErrTimeout {

			stats.Add("retryCount", 1)
			log.Warnf("Retrying #%d publishing to NATS", i)
			h.stanConn = nil

			continue
		}

		log.Errorf("Publishing: %s", err)
		stats.Add("errorCount", 1)
	}
}

// handlReceive is the HTTP endpoint that accepts log messages and send them on to
// the message broker for further processing.
func (h *HttpRelay) handleReceive(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	data, bytesRead, err := h.readAll(req.Body)

	stats.Add("receivedCount", 1)

	// See if the circuit breaker is flipped on, push back to clients
	// instead of attempting to queue and then dropping the message.
	if !h.isAvailable() {
		// Lazily try to reconnect and reset the breaker
		h.connectStan()

		if !h.isAvailable() {
			log.Error("Breaker is off, refusing message!")
			stats.Add("refusedCount", 1)
			http.Error(response, `{"status": "error", "message": "Invalid NATS connection"}`, 502)
			return
		}
	}

	if err != nil {
		stats.Add("errorCount", 1)
		log.Error(err)
	}

	var msg message.Message
	// Using a length limit is important here! Otherwise msg will include
	// all the null chars at the end of the byte slice.
	proto.Unmarshal(data[:bytesRead], &msg)

	defer func() {
		h.pool.Put(data)
		stats.Add("returnedPool", 1)
	}()

	if (msg.Payload == nil && len(msg.Fields) == 0) || msg.Hostname == nil {
		log.Warnf("Missing fields! %s", msg.String())
		stats.Add("skipped", 1)
		return
	}

	niceTime := time.Unix(0, *msg.Timestamp)
	if msg.Payload == nil {
		fmt.Printf("%s\n", msg.Fields)
	} else {
		fmt.Printf("%s %s: %#v\n", niceTime, *msg.Hostname, *msg.Payload)
	}
	if h.matcher == nil || h.matcher.Match(&msg) {
		h.relayMessage(&msg)
	}
}

// readAll will fetch as much as possible from the reader into a buffer
// from the pool. It will stop reading when the buffer is full and
// truncate the result. This is logged as a warning.
func (h *HttpRelay) readAll(r io.Reader) (b []byte, bytesRead int, err error) {
	buf := h.pool.Get()
	stats.Add("getPool", 1)

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
	var relay HttpRelay
	err := envconfig.Process("rcvr", &relay)
	if err != nil {
		log.Fatalf("Unable to start: %s", err)
	}

	rubberneck.Print(relay)

	err = relay.Relay()
	if err != nil {
		log.Fatal(err.Error())
	}
}
