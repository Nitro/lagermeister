package main

import (
	"bytes"
	"fmt"
	"net"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	"github.com/gogo/protobuf/proto"
)

// TODO buffer pool for input buffer!

type TcpListener struct {
	Address   string
	OnConnect func(net.Conn) error
}

func (t *TcpListener) Listen() {
	listener, err := net.Listen("tcp", t.Address)
	if err != nil {
		log.Fatal("Error starting TCP server.")
	}
	defer listener.Close()

	for {
		conn, _ := listener.Accept()
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
		bytesRead = len(buffer)
		return bytesRead, nil, nil // read more data to find the start of the next message
	}

	if len(buffer) < bytesRead+message.HEADER_DELIMITER_SIZE {
		return bytesRead, nil, nil // read more data to get the header length byte
	}

	headerLength := int(buffer[bytesRead+1])
	headerEnd := bytesRead + headerLength + message.HEADER_FRAMING_SIZE
	if len(buffer) < headerEnd {
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
		messageEnd := headerEnd + int(header.GetMessageLength())
		if len(buffer) < messageEnd {
			return // read more data to get the remainder of the message
		}
		record = buffer[bytesRead:messageEnd]
		bytesRead = messageEnd

		err = proto.Unmarshal(record, &msg)
		if err != nil {
			panic("ASDFSADF " + err.Error())
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
	// TODO buffer pool this! Don't create all the time
	buffer := make([]byte, 2048)

	readLen, err := conn.Read(buffer)

	if err == nil {
		bytesRead, record, header := t.FindRecord(buffer)
		//var header message.Header

		//headerLen := int(buffer[0]) + message.HEADER_FRAMING_SIZE
		//println(headerLen)

		//ok, err := message.DecodeHeader(buffer[2:headerLen], &header)
		//if !ok && err != nil {
		//	panic(err)
		//}
		fmt.Printf("Read: %d\nRecord Read: %d\nMsg Length: %d\nHeader: %#v\n%s\nRecord: %#v\n",
			readLen, bytesRead, header.MessageLength, header, string(buffer), record,
		)
		println(record)
	} else {
		log.Errorf("Unable to read from socket: %s", err)
	}

	conn.Close()
}

func main() {
	listener := &TcpListener{Address: "0.0.0.0:35000"}
	listener.Listen()
}
