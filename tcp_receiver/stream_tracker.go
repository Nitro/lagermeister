package main

import (
	"bytes"
	"errors"
	"io"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	"github.com/gogo/protobuf/proto"
	"github.com/oxtoacart/bpool"
)

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

func NewStreamTracker(pool *bpool.BytePool) *StreamTracker {
	stream := &StreamTracker{Pool: pool}
	stream.buf = pool.Get()
	stats.Add("getPool", 1)
	stream.Reset()

	return stream
}

func (s *StreamTracker) Read(conn io.Reader) (keepProcessing bool, finished bool) {
	var (
		err    error
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
		keepProcessing = false
		finished = true
		return
	}

	// TODO move this farther down so we can complete the last message!
	if err == io.EOF {
		// Causes us to close the socket outside the loop
		keepProcessing = false
		finished = true
		return
	}

	keepProcessing = true
	finished = false
	return
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
	if s.firstHeader < 0 {
		return errors.New("ParseHeader called before FindHeader!")
	}

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
	if s.firstMessage < 0 {
		return false, errors.New("ParseMessage called before FindMessage!")
	}

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

func (s *StreamTracker) GetMessage() *message.Message {
	return &s.msg
}

// Reset clears all the tracking information for the current message
func (s *StreamTracker) Reset() {
	s.readLen = 0
	s.expectedLen = -1
	s.header = message.Header{} // XXX this should probably be pooled...
	s.firstHeader = -1
	s.firstMessage = -1
	s.foundHeader = false
}

func (s *StreamTracker) CleanUp() {
	// Put the buffer back in the buffer pool
	s.Pool.Put(s.buf)
	stats.Add("returnedPool", 1)
}

// readSocket is a light wrapper around conn.Read
func readSocket(conn io.Reader, buf []byte) (int, error) {
	readLen, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		stats.Add("readError", 1)
	}

	return readLen, err
}
