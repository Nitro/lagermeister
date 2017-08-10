package main

import (
	"bytes"
	"expvar"
	"io/ioutil"
	"net"
	"time"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	"github.com/gogo/protobuf/proto"
	"github.com/oxtoacart/bpool"
)

// TODO need to make sure we finish processing each connection

const (
	DefaultPoolSize       = 100
	DefaultPoolMemberSize = 21 * 1024
	DefaultKeepAlive      = 30 * time.Second
)

var (
	stats = expvar.NewMap("stats")
)

type TcpListener struct {
	Address           string
	OnConnect         func(net.Conn) error
	KeepAlive         bool
	KeepAliveDuration time.Duration
	pool              *bpool.BytePool
}

func (t *TcpListener) Listen() {
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
				//tcpConn, ok := conn.(*net.TCPConn)
				//if !ok {
				//	// Not sure how we'd get here without an error in the stdlib.
				//	log.Warn("Keepalive only supported on TCP, got something else!")
				//	continue
				//}

				//tcpConn.SetKeepAlive(t.KeepAlive)
				//if t.KeepAliveDuration != 0 {
				//	tcpConn.SetKeepAlivePeriod(t.KeepAliveDuration)
				//}

				go func() {
					if t.OnConnect != nil {
						if err := t.OnConnect(conn); err != nil {
							log.Errorf("Error on connect, disconnecting: %s", err)
							conn.Close()
							return
						}
					}

					go t.handleConnection(conn)
				}()
			}
		}()
	}

	select {}
}

func (t *TcpListener) UnframeRecord(framed []byte) []byte {
	headerLen := int(framed[1]) + message.HEADER_FRAMING_SIZE
	unframed := framed[headerLen:]
	return unframed
}

func (t *TcpListener) FindRecord(buffer []byte) (bytesRead int, record []byte, header *message.Header) {

	header = &message.Header{}

	bytesRead = bytes.IndexByte(buffer, message.RECORD_SEPARATOR)
	if bytesRead == -1 {
		log.Debug("No record separator found")
		bytesRead = len(buffer)
		return bytesRead, nil, nil // read more data to find the start of the next message
	}

	if len(buffer) < bytesRead+message.HEADER_DELIMITER_SIZE {
		log.Debugf("Buffer too small!: %d", len(buffer))
		return bytesRead, nil, nil // read more data to get the header length byte
	}

	headerLength := int(buffer[bytesRead+1])
	log.Debugf("Header length: %d", headerLength)

	headerEnd := bytesRead + headerLength + message.HEADER_FRAMING_SIZE
	if len(buffer) < headerEnd {
		log.Debugf("Buffer too small, unable to read header!: %d", len(buffer))
		return bytesRead, nil, nil // read more data to get the remainder of the header
	}

	hasHeader, err := message.DecodeHeader(
		buffer[bytesRead+message.HEADER_DELIMITER_SIZE:headerEnd],
		header,
	)

	if err != nil {
		log.Errorf("Error finding record: %s", err)
	}

	var msg message.Message

	if header.MessageLength != nil || hasHeader {
		log.Debugf("Message length: %d", *header.MessageLength)
		messageEnd := headerEnd + int(header.GetMessageLength())
		if len(buffer) < messageEnd {
			return // read more data to get the remainder of the message
		}
		record = buffer[headerEnd:messageEnd]
		ioutil.WriteFile("/Users/kmatthias/out.pbuf", record, 0644)
		bytesRead = messageEnd

		err = proto.Unmarshal(record, &msg)
		if err != nil {
			log.Errorf("Unable to deserialize Protobuf record: %s ", err)
		}

		header.Reset()
	} else {
		var n int
		bytesRead++ // advance over the current record separator
		n, record, header = t.FindRecord(buffer[bytesRead:])
		bytesRead += n
	}

	return bytesRead, record, header
}

func (t *TcpListener) handleConnection(conn net.Conn) {
	buffer := t.pool.Get()

	defer func() {
		t.pool.Put(buffer)
		stats.Add("returnedPool", 1)
	}()

	stats.Add("receivedCount", 1)

	readLen, err := conn.Read(buffer)

	log.Infof("Read %d bytes", readLen)

	if err == nil {
		bytesRead, record, header := t.FindRecord(buffer)

		log.Debugf("Read: %d\nRecord Read: %d\nMsg Length: %d\nHeader: %#v\n%s\nRecord: %#v\n",
			readLen, bytesRead, header.MessageLength, header, string(buffer), string(record),
		)
	} else {
		log.Errorf("Unable to read from socket: %s", err)
	}

	conn.Close()
}

func main() {
	log.SetLevel(log.DebugLevel)

	listener := &TcpListener{
		Address:           "0.0.0.0:35000",
		KeepAlive:         true,
		KeepAliveDuration: DefaultKeepAlive,
	}

	listener.Listen()
}
