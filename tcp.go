package main

import (
	"bytes"
	"expvar"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	"github.com/gogo/protobuf/proto"
	"github.com/oxtoacart/bpool"
)

// TODO need to make sure we finish processing each connection

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

func (t *TcpRelay) Listen() {
	t.pool = bpool.NewBytePool(DefaultPoolSize, DefaultPoolMemberSize)

	listener, err := net.Listen("tcp", t.Address)
	if err != nil {
		log.Fatal("Error starting TCP server.")
	}
	defer listener.Close()

	for i := 0; i < 20; i++ {
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

// readSocket is a light wrapper around conn.Read
func readSocket(conn net.Conn, buf []byte) (int, error) {
	readLen, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		stats.Add("readError", 1)
	}

	return readLen, err
}

type StreamTracker struct {
	expectedLen  int
	readLen      int
	buf          []byte
	header       message.Header
	msg          message.Message
	firstHeader  int
	firstMessage int
	foundHeader  bool
	Pool         *bpool.BytePool
}

func (s *StreamTracker) Read(conn net.Conn) (keepProcessing bool, finished bool) {
	var (
		err       error
		nBytes int
	)

	log.Debugf("readLen: %d", s.readLen)

	if s.expectedLen > 0 {
		nBytes, err = readSocket(conn, s.buf[s.readLen:s.expectedLen])
	} else {
		nBytes, err = readSocket(conn, s.buf[s.readLen:])
	}
	s.readLen += nBytes

	log.Debugf("nBytes: %d", nBytes)
	if err != nil && err != io.EOF {
		log.Errorf("Unable to read from socket: %s", err)
		// Return so we don't even try to close the socket
		return false, true
	}

	// TODO move this farther down so we can complete the last message!
	if err == io.EOF {
		// Causes us to close the socket outside the loop
		return false, true
	}

	return true, false
}

// FindHeaderMarker returns the first header found in the stream
func (s *StreamTracker) FindHeader() (found bool) {
	s.firstHeader = bytes.IndexByte(s.buf[:s.readLen], message.RECORD_SEPARATOR)
	if s.firstHeader != -1 {
		return true
	}

	return false
}

// FindMessageMarker returns the first message found in the stream
func (s *StreamTracker) FindMessage() (found bool) {
	s.firstMessage = bytes.IndexByte(s.buf[:s.readLen], message.UNIT_SEPARATOR)
	if s.firstMessage != -1 {
		return true
	}

	return false
}

func (s *StreamTracker) IsValid() bool {
	log.Debugf("firstHeader: %d, firstMsg: %d", s.firstHeader, s.firstMessage)
	// The header should always be before the message
	return s.firstHeader < s.firstMessage
}

// ParseHeader parses a Heka framing header. The format of a header is like this:
//
// Record Sep     | 0x1E
// Header Len     | ..         <-- In practice only the 1st byte is used
// Header Bytes   | .......
// Header Framing | ..
// Unit Sep       | 0x1F
//
// We only want to pass the Header Bytes to message.DecodeHeader.
func (s *StreamTracker) ParseHeader() error {
	headerLength := int(s.buf[s.firstHeader+1])
	headerStart := s.firstHeader + message.HEADER_DELIMITER_SIZE
	headerEnd := s.firstHeader + headerLength + message.HEADER_FRAMING_SIZE

	_, err := message.DecodeHeader(
		s.buf[headerStart:headerEnd],
		&s.header,
	)
	if err != nil {
		return err
	}

	// Now that we have a header, we can tell how long this message is
	s.expectedLen = s.firstMessage + int(s.header.GetMessageLength()) + 1

	return nil
}

// parseMessage takes a byte slice and unmarshals the Heka message it contains
func (s *StreamTracker) ParseMessage() (ok bool, err error) {
	messageStart := s.firstMessage + 1 // Skip the unit separator
	messageEnd := messageStart + int(s.header.GetMessageLength())

	err = proto.Unmarshal(s.buf[messageStart:messageEnd], &s.msg)
	if err != nil {
		return false, err
	}

	// If we got a message but we don't have the important fields
	if (s.msg.Payload == nil && len(s.msg.Fields) == 0) || s.msg.Hostname == nil {
		log.Warnf("Missing fields! %s", s.msg.String())
		return false, nil
	}

	log.Debugf("Message: %#v", s.msg)
	log.Debugf("Hostname: %v", *s.msg.Hostname)

	return true, nil
}

func (s *StreamTracker) HasEnoughCapacity() bool {
	return s.expectedLen < cap(s.buf)
}

func (s *StreamTracker) HasReadEntireMessage() bool {
	return s.readLen >= s.expectedLen
}

func (s *StreamTracker) HandleOverread() (hadOverread bool) {
	if s.readLen <= s.expectedLen {
		return false
	}

	log.Debugf("Over-read %d bytes, cycling buffer", s.readLen-s.expectedLen)

	stats.Add("getPool", 1)
	buf2 := s.Pool.Get()
	copy(buf2, s.buf[s.expectedLen:s.readLen])
	s.Pool.Put(s.buf)
	stats.Add("returnedPool", 1)
	s.buf = buf2

	// Reset the counters to the new length
	tmpReadLen := s.readLen - s.expectedLen
	s.Reset()
	s.readLen = tmpReadLen

	return true
}

// Reset clears all the tracking information for the current message
func (s *StreamTracker) Reset() {
	s.readLen = 0
	s.expectedLen = -1
	s.header = message.Header{}
	s.firstHeader = -1
	s.firstMessage = -1
	s.foundHeader = false
}

func NewStreamTracker(pool *bpool.BytePool) *StreamTracker {
	stream := &StreamTracker{Pool: pool}
	stream.buf = pool.Get()
	stats.Add("getPool", 1)
	stream.Reset()

	return stream
}

func (s *StreamTracker) CleanUp() {
	// Put the buffer back in the buffer pool
	s.Pool.Put(s.buf)
	stats.Add("returnedPool", 1)
}

// handleConnection handles the main work of the program, processing the
// incoming data stream.
func (t *TcpRelay) handleConnection(conn net.Conn) {

	stats.Add("connectCount", 1)
	stream := NewStreamTracker(t.pool)
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

	listener.Listen()
}
