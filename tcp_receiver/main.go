package main

import (
	"expvar"
	"fmt"
	"io"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Nitro/lagermeister/event"
	"github.com/Nitro/lagermeister/message"
	"github.com/Nitro/lagermeister/publisher"
	"github.com/kelseyhightower/envconfig"
	"github.com/oxtoacart/bpool"
	log "github.com/sirupsen/logrus"
	"gopkg.in/relistan/rubberneck.v1"
)

const (
	DefaultPoolSize       = 100       // 100 in the default pool
	DefaultPoolMemberSize = 10 * 1024 // 10KB buffers
	DefaultKeepAlive      = 30 * time.Second
)

var (
	stats     = expvar.NewMap("stats")
	sentCount = int64(0)
)

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

type TcpRelay struct {
	Address         string        `envconfig:"BIND_ADDRESS" default:":35000"`
	NatsUrl         string        `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	ClusterId       string        `envconfig:"CLUSTER_ID" default:"test-cluster"`
	ClientId        string        `envconfig:"CLIENT_ID" required:"true"`
	Subject         string        `envconfig:"SUBJECT" default:"lagermeister-test"`
	MatchSpec       string        `envconfig:"MATCH_SPEC"` // Heka message matcher
	StatsAddress    string        `envconfig:"STATS_ADDRESS" default:":34999"`
	ListenCount     int           `envconfig:"LISTEN_COUNT" default:"20"`
	LoggingLevel    string        `envconfig:"LOGGING_LEVEL" default:"info"`
	ConnectHoldDown time.Duration `envconfig:"CONNECT_HOLD_DOWN" default:"10s"`

	KeepAlive         bool
	KeepAliveDuration time.Duration

	pool           *bpool.BytePool
	matcher        *message.MatcherSpecification
	connection     publisher.Publisher
	initialized    bool
	quitChan       chan struct{}
	MetricReporter *event.MetricReporter
}

func (t *TcpRelay) init() error {
	t.pool = bpool.NewBytePool(DefaultPoolSize, DefaultPoolMemberSize)
	// Set up the publisher, passing along the configuration
	t.connection = &publisher.StanPublisher{
		NatsUrl:         t.NatsUrl,
		ClusterId:       t.ClusterId,
		ClientId:        t.ClientId,
		Subject:         t.Subject,
		Stats:           stats,
		ConnectHoldDown: t.ConnectHoldDown,
	}
	err := t.connection.Connect()
	if err != nil {
		return fmt.Errorf("Error starting NATS connection: %s", err)
	}

	t.quitChan = make(chan struct{})
	t.initialized = true

	return nil
}

func (t *TcpRelay) Listen() error {
	if !t.initialized {
		err := t.init()
		if err != nil {
			return err
		}
	}

	listener, err := net.Listen("tcp", t.Address)
	if err != nil {
		return fmt.Errorf("Error starting TCP server: %s", err)
	}

	// We have a fixed number of listeners, each of which will spawn a goroutine
	// for each incoming connection. That goroutine will be dedicated to the
	// connection for its lifesspan and will exit afterward.
	for i := 0; i < t.ListenCount; i++ {
		go func() {
			for {
				select {
				case <-t.quitChan:
					break
				default:
				}
				conn, err := listener.Accept()
				if err != nil {
					log.Errorf("Error accepting connection! (%s)", err)
					continue
				}

				// This ought to be a TCP connection, and we want to set some
				// connection settings, like keepalive. So cast and catch the
				// error if it's not.
				tcpConn, ok := conn.(*net.TCPConn)
				if !ok {
					// Not sure how we'd get here without an error in the stdlib.
					log.Warn("Keepalive only supported on TCP, got something else! (%#v)", conn)
					continue
				}

				// We want to do Keepalive on these connections, so set it up
				tcpConn.SetKeepAlive(t.KeepAlive)
				if t.KeepAliveDuration != 0 {
					tcpConn.SetKeepAlivePeriod(t.KeepAliveDuration)
				}

				go t.handleConnection(conn)
			}
		}()
	}

	return nil
}

// handleConnection handles the main work of the program, processing the
// incoming data stream.
func (t *TcpRelay) handleConnection(conn io.ReadCloser) {

	stats.Add("connectCount", 1)
	stream := NewStreamTracker(t.pool) // Allocate a StreamTracker and get buffer from pool
	defer stream.CleanUp()             // Return buffer to pool when we're done

	var keepProcessing, finished bool
	var msg *message.Message

	for {
		log.Debug("---------------------")
		var err error

		// Try to read from the stream and deal with the result
		keepProcessing, finished = stream.Read(conn)
		if finished {
			break
		}
		if !keepProcessing {
			continue
		}

		if !stream.FindHeader() || !stream.FindMessage() {
			// Just need to read more data
			continue
		}

		// This happens if we got an unparseable header
		if !stream.IsValid() {
			stats.Add("invalidCount", 1)
			log.Warn("Skipping invalid message")
			stream.Reset()
			continue
		}

		// We should now have a header, so let's parse it
		err = stream.ParseHeader()
		if err != nil {
			log.Warnf("Unable to parse header: %s", err)
			stream.Reset()
			continue
		}

		if !stream.HasEnoughCapacity() {
			log.Warn("Dropping message since it would exceed buffer capacity")
			stream.Reset()
			continue
		}

		// Read until we have enough to deserialize the body
		if !stream.HasReadEntireMessage() {
			// We could optimize here by reading again in a loop so we
			// don't re-parse the header. But, once the stream is warmed up,
			// we almost always get a whole message on the first read. So
			// optimizing is kinda silly.
			log.Debug("Not enough data, reading more")
			continue
		}

		// We got the whole thing, so count it
		stats.Add("receivedCount", 1)
		atomic.AddInt64(&sentCount, 1)

		ok, err := stream.ParseMessage()
		if err != nil {
			stats.Add("skipped", 1)
			log.Warnf("Unable to deserialize protobuf message: %s", err)
			stream.Reset()
			continue
		}
		if !ok {
			stats.Add("skipped", 1)
			log.Warn("Nil message or missing required fields!")
			stream.Reset()
			continue
		}

		// This has to happen before cleaning up the buffer.
		msg = stream.GetMessage()
		if t.matcher == nil || t.matcher.Match(msg) {
			// XXX we can substantially improve performance by handling
			// message relaying in a thread pool instead of on the main
			// goroutine. Will involve holding onto the buffer, and would
			// require different behavior here.
			t.connection.RelayMessage(msg)
		}

		// If we took in more than one message in this read, we need to get a new
		// buffer and populate it with the remaining data and set the readLen.
		if !stream.HandleOverread() {
			stream.Reset()
		}
	}

	log.Info("Disconnecting socket")
	conn.Close()
}

// reportThroughput runs in the background and reports a throughput metric to the stats
// system.
func (t *TcpRelay) reportThroughput() {
	for {
		select {
		case <-time.After(1 * time.Second):
			t.sendThroughputMetric()
		case <-t.quitChan:
			return
		}
	}
}

func (t *TcpRelay) sendThroughputMetric() {
	count := atomic.SwapInt64(&sentCount, 0)
	statsTput := new(expvar.Int)
	statsTput.Set(count)
	stats.Set("throughput", statsTput)

	if t.MetricReporter != nil {
		t.MetricReporter.TrySendMetrics(&event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			Value:      float64(count),
			Sender:     "tcp-receiver", // TODO make this configurable
			MetricType: "Throughput",
		})
	}
}

// Set up some signal handling for kill/term/int and try to disconnect
// NATS client
func handleSignals(t *TcpRelay) {
	sigChan := make(chan os.Signal, 1) // Buffered!

	// Grab some signals we want to catch where possible
	signal.Notify(sigChan, os.Interrupt)
	signal.Notify(sigChan, os.Kill)
	signal.Notify(sigChan, syscall.SIGTERM)

	sig := <-sigChan
	log.Warnf("Received signal '%s', attempting clean shutdown", sig)

	close(t.quitChan)
	t.quitChan = nil
	t.connection.Shutdown()
	time.Sleep(1 * time.Second) // Wait for it to close the connections

	log.Info("Shut down.")
	os.Exit(130) // Ctrl-C received or equivalent
}

func main() {
	var relay TcpRelay
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		envconfig.Usage("tcprcvr", &relay)
		os.Exit(1)
	}
	err := envconfig.Process("tcprcvr", &relay)
	if err != nil {
		log.Fatal(err)
	}
	relay.KeepAlive = true
	relay.KeepAliveDuration = DefaultKeepAlive

	printer := rubberneck.NewPrinter(log.Infof, rubberneck.NoAddLineFeed)
	printer.PrintWithLabel("tcp_receiver settings", relay)

	reporter := event.NewMetricReporter()
	err = reporter.ProcessMetrics()
	if err != nil {
		log.Fatalf("Unable to connect to NATS for stats reporting! (%s)", err)
	}
	relay.MetricReporter = reporter

	configureLoggingLevel(relay.LoggingLevel)

	// Stats relay
	go relay.reportThroughput()
	go http.ListenAndServe(relay.StatsAddress, nil)

	err = relay.Listen()
	if err != nil {
		log.Fatal(err)
	}

	handleSignals(&relay)

	select {}
}
