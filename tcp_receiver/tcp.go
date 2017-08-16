package main

import (
	"expvar"
	"net"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/oxtoacart/bpool"
)

const (
	DefaultPoolSize       = 100
	DefaultPoolMemberSize = 22 * 1024
	DefaultKeepAlive      = 30 * time.Second
)

var (
	stats = expvar.NewMap("stats")
)

type TcpRelay struct {
	Address           string
	KeepAlive         bool
	KeepAliveDuration time.Duration
	pool              *bpool.BytePool
}

func (t *TcpRelay) Listen(numListeners int) {
	t.pool = bpool.NewBytePool(DefaultPoolSize, DefaultPoolMemberSize)

	listener, err := net.Listen("tcp", t.Address)
	if err != nil {
		log.Fatal("Error starting TCP server.")
	}
	defer listener.Close()

	// We have a fixed number of listeners, each of which will spawn a goroutine
	// for each incoming connection. That goroutine will be dedicated to the
	// connection for its lifesspan and will exit afterward.
	for i := 0; i < numListeners; i++ {
		go func() {
			for {
				conn, _ := listener.Accept()

				// This ought to be a TCP connection, and we want to set some
				// connection settings, like keepalive. So cast and catch the
				// error if it's not.
				tcpConn, ok := conn.(*net.TCPConn)
				if !ok {
					// Not sure how we'd get here without an error in the stdlib.
					log.Warn("Keepalive only supported on TCP, got something else!")
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

	select {}
}

// handleConnection handles the main work of the program, processing the
// incoming data stream.
func (t *TcpRelay) handleConnection(conn net.Conn) {

	stats.Add("connectCount", 1)
	stream := NewStreamTracker(t.pool) // Allocate a StreamTracker and get buffer from pool
	defer stream.CleanUp() // Return buffer to pool when we're done

	for {
		log.Debug("---------------------")
		var err error

		// Try to read from the stream and deal with the result
		keepProcessing, finished := stream.Read(conn)
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

		// TODO relay the message here, before recycling the buffer!
		// Once the buffer is recycled, pointers into it are non-deterministic

		// If we took in more than one message in this read, we need to get a new
		// buffer and populate it with the remaining data and set the readLen.
		if !stream.HandleOverread() {
			stream.Reset()
		}
	}

	log.Info("Disconnecting socket")
	conn.Close()
}

func main() {
	log.SetLevel(log.DebugLevel)

	// Stats listener
	go http.ListenAndServe("0.0.0.0:34999", nil)

	listener := &TcpRelay{
		Address:           "0.0.0.0:35000",
		KeepAlive:         true,
		KeepAliveDuration: DefaultKeepAlive,
	}

	listener.Listen(20)
}
