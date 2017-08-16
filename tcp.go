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

// parseHeader parses a Heka framing header. The format of a header is like this:
//
// Record Sep     | 0x1E
// Header Len     | ..         <-- In practice only the 1st byte is used
// Header Bytes   | .......
// Header Framing | ..
// Unit Sep       | 0x1F
//
// We only want to pass the Header Bytes to message.DecodeHeader.
func parseHeader(start int, buf []byte) (*message.Header, error) {
	headerLength := int(buf[start+1])
	headerStart := start + message.HEADER_DELIMITER_SIZE
	headerEnd := start + headerLength + message.HEADER_FRAMING_SIZE

	var header message.Header

	_, err := message.DecodeHeader(
		buf[headerStart:headerEnd],
		&header,
	)
	if err != nil {
		return nil, err
	}

	return &header, nil
}

// parseMessage takes a byte slice and unmarshals the Heka message it contains
func parseMessage(start int, length int, buf []byte) (*message.Message, error) {
	var msg message.Message
	messageStart := start + 1 // Skip the unit separator
	messageEnd := messageStart + length

	err := proto.Unmarshal(buf[messageStart:messageEnd], &msg)
	if err != nil {
		log.Warnf("Unable to deserialize protobuf message: %s", err)
		return nil, err
	}

	return &msg, nil
}

// readSocket is a light wrapper around conn.Read
func readSocket(conn net.Conn, buf []byte) (int, error) {
	readLen, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		stats.Add("readError", 1)
	}

	return readLen, err
}

// handleConnection handles the main work of the program, processing the
// incoming data stream.
func (t *TcpRelay) handleConnection(conn net.Conn) {
	buf := t.pool.Get()
	stats.Add("getPool", 1)

	// Return the buffer to the pool and keep stats about pool use
	defer func() {
		t.pool.Put(buf)
		stats.Add("returnedPool", 1)
	}()

	var readLen, expectedLen int

	// Just resets the readLen/expectedLen and re-uses the same buffer
	reset := func() {
		readLen = 0
		expectedLen = -1
	}

	stats.Add("connectCount", 1)

OUTER:
	for {
		log.Debug("---------------------")
		log.Debugf("readLen: %d", readLen)

		// Only read up to the expectedLen, so we can use a complete buffer for
		// the next message.
		var nBytes int
		var err error

		if expectedLen > 0 {
			nBytes, err = readSocket(conn, buf[readLen:expectedLen])
		} else {
			nBytes, err = readSocket(conn, buf[readLen:])
		}
		readLen += nBytes

		log.Debugf("nBytes: %d", nBytes)
		if err != nil && err != io.EOF {
			log.Errorf("Unable to read from socket: %s", err)
			// Return so we don't even try to close the socket
			return
		}

		// TODO move this farther down so we can complete the last message!
		if err == io.EOF {
			// Causes us to close the socket outside the loop
			break OUTER
		}

		firstHeader := bytes.IndexByte(buf[:readLen], message.RECORD_SEPARATOR)
		firstMsg := bytes.IndexByte(buf[:readLen], message.UNIT_SEPARATOR)

		// Do we have a complete header? If so, parse it!
		var header *message.Header
		if firstHeader == -1 && firstMsg == -1 {
			// Just need to read more data
			continue
		}

		// This happens if we got an unparseable header
		if firstMsg < firstHeader {
			stats.Add("invalidCount", 1)
			log.Warn("Skipping invalid message")
			reset()
			continue
		}

		log.Debugf("firstHeader: %d, firstMsg: %d", firstHeader, firstMsg)

		header, err = parseHeader(firstHeader, buf[:readLen])
		if err != nil {
			log.Warnf("Unable to parse header: %s", err)
		}

		if header.GetMessageLength() < 1 {
			header, err = parseHeader(firstHeader, buf[1:readLen])
			log.Warn("Header was not valid, missing message length")
			reset()
			continue
		}

		expectedLen = firstMsg + int(header.GetMessageLength()) + 1
		log.Debugf("Expected Length: %d", expectedLen)

		if expectedLen > cap(buf) {
			log.Warn("Dropping message since it would exceed buffer capacity")
			reset()
			continue
		}

		// Read until we have enough to deserialize the body
		if readLen < expectedLen {
			// We could optimize here by reading again in a loop so we
			// don't re-parse the header. But seems reasonable without that.
			log.Debug("Not enough data, reading more")
			continue
		}

		stats.Add("receivedCount", 1)

		// parseMessage will limit the read length internally
		msg, _ := parseMessage(firstMsg, int(header.GetMessageLength()), buf)
		if msg == nil {
			log.Error("Nil message!")
			reset()
			continue
		}
		if (msg.Payload == nil && len(msg.Fields) == 0) || msg.Hostname == nil {
			log.Warnf("Missing fields! %s", msg.String())
			stats.Add("skipped", 1)
			reset()
			continue
		}

		log.Debugf("Message: %#v", msg)
		log.Debugf("Hostname: %v", *msg.Hostname)

		// TODO relay the message here, before recycling the buffer!

		// If we took in more than one message in this read, we need to get a new
		// buffer and populate it with the remaining data. We also need to set the
		// readLen to the correct new length.
		if (expectedLen > 0) && (readLen > expectedLen) {
			log.Warnf("Over-read %d bytes, cycling", readLen - expectedLen)
			stats.Add("getPool", 1)
			buf2 := t.pool.Get()
			copy(buf2, buf[expectedLen:readLen])
			t.pool.Put(buf)
			stats.Add("returnedPool", 1)
			buf = buf2

			// Reset the counters to the new length and expectedLength
			readLen = readLen - expectedLen
			expectedLen = -1
		} else {
			reset()
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
