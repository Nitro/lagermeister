package main

import (
	"expvar"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Nitro/lagermeister/message"
	"github.com/Nitro/lagermeister/publisher"
	"github.com/gogo/protobuf/proto"
	"github.com/kelseyhightower/envconfig"
	"github.com/oxtoacart/bpool"
	log "github.com/sirupsen/logrus"
	"gopkg.in/relistan/rubberneck.v1"
)

const (
	DefaultPoolSize       = 100
	DefaultPoolMemberSize = 21 * 1024
)

var (
	stats = expvar.NewMap("stats")
)

type HttpRelay struct {
	Address         string        `envconfig:"BIND_ADDRESS" default:":35001"`
	NatsUrl         string        `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	ClusterId       string        `envconfig:"CLUSTER_ID" default:"test-cluster"`
	ClientId        string        `envconfig:"CLIENT_ID" required:"true"`
	Subject         string        `envconfig:"SUBJECT" default:"lagermeister-test"`
	MatchSpec       string        `envconfig:"MATCH_SPEC"` // Heka message matcher
	ConnectHoldDown time.Duration `envconfig:"CONNECT_HOLD_DOWN" default:"10s"`

	matcher    *message.MatcherSpecification
	pool       *bpool.BytePool
	connection publisher.Publisher
}

func (h *HttpRelay) init() error {
	var err error

	if len(h.MatchSpec) > 0 {
		h.matcher, err = message.CreateMatcherSpecification(h.MatchSpec)
		if err != nil {
			return fmt.Errorf("Unable to parse matcher spec: %s", err)
		}
	}

	h.pool = bpool.NewBytePool(DefaultPoolSize, DefaultPoolMemberSize)

	if h.connection == nil { // We can provide our own (e.g. as a mock)
		h.connection = &publisher.StanPublisher{
			NatsUrl:         h.NatsUrl,
			ClusterId:       h.ClusterId,
			ClientId:        h.ClientId,
			Subject:         h.Subject,
			ConnectHoldDown: h.ConnectHoldDown,
			Stats:           stats,
		}
		h.connection.Connect()
	}

	return nil
}

// Relay sets up the HTTP listener and the NATS client, then starts
// handling traffic between the HTTP input and NATS output.
func (h *HttpRelay) Relay() error {
	err := h.init()
	if err != nil {
		return fmt.Errorf("Unable to initialize relay: %s", err)
	}

	http.HandleFunc("/", h.handleReceive)
	http.HandleFunc("/health", h.handleHealth)

	err = http.ListenAndServe(h.Address, nil)
	if err != nil {
		return fmt.Errorf("HTTP listener: %s", err)
	}

	return nil
}

// handlReceive is the HTTP endpoint that accepts log messages and send them on to
// the message broker for further processing.
func (h *HttpRelay) handleReceive(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	if req.Method != "POST" {
		http.Error(response, `{"status": "error", "message": "Invalid request action! Expected POST."}`, 405)
		log.Warnf("Got request with action '%s', expected POST.", req.Method)
		return
	}

	data, bytesRead, err := h.readAll(req.Body) // data is a pooled buffer!

	// Make sure we put the buffer we got from readAll back in the pool!
	defer func() {
		h.pool.Put(data)
		stats.Add("returnedPool", 1)
	}()

	stats.Add("receivedCount", 1)
	response.Header().Set("Content-Type", "application/json")

	if err != nil { // this is from readAll above
		stats.Add("errorCount", 1)
		// If we have a socket error this may not get sent... but we'll try
		http.Error(response, `{"status": "error", "message": "Socket read error"}`, 500)
		log.Errorf("Reading from socket: %s", err)
		return
	}

	// See if the circuit breaker is flipped on, push back to clients
	// instead of attempting to queue and then dropping the message.
	if !h.connection.IsAvailable() {
		// Lazily try to reconnect and reset the breaker
		h.connection.Connect()

		if !h.connection.IsAvailable() {
			log.Error("Breaker is on, refusing message!")
			stats.Add("refusedCount", 1)
			http.Error(response, `{"status": "error", "message": "Invalid NATS connection"}`, 502)
			return
		}
	}

	var msg message.Message
	// Using a length limit is important here! Otherwise msg will include
	// all the null chars at the end of the byte slice.
	err = proto.Unmarshal(data[:bytesRead], &msg)
	if err != nil { // this is from readAll above
		stats.Add("errorCount", 1)
		http.Error(response, `{"status": "error", "message": "Invalid message sent"}`, 400)
		log.Errorf("Unmarshaling protobuf: %s", err)
		return
	}

	if (msg.Payload == nil && len(msg.Fields) == 0) || msg.Hostname == nil {
		log.Warnf("Missing fields in request from %s! %s", req.RemoteAddr, msg.String())
		stats.Add("skipped", 1)
		return
	}

	log.Debugf("Hostname: %s Payload: %#v\n", *msg.Hostname, *msg.Payload)

	if h.matcher == nil || h.matcher.Match(&msg) {
		h.connection.RelayMessage(&msg)
	}

	// In the acceptable case we don't reply with anything but a 200 OK status
}

func (h *HttpRelay) handleHealth(response http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	response.Header().Set("Content-Type", "application/json")

	if !h.connection.IsAvailable() {
		http.Error(response, `{"status": "error", "message": "Circuit breaker is flipped!"}`, 502)
		return
	}

	expvar.Do(func(kv expvar.KeyValue) {
		if kv.Key == "stats" {
			response.Write([]byte(kv.Value.String()))
		}
	})
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
			return buf, 0, err
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

	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		envconfig.Usage("rcvr", &relay)
		os.Exit(1)
	}

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
