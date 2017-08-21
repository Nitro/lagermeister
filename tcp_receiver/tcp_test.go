package main

import (
	"io"
	"os"
	"testing"

	"github.com/Nitro/lagermeister/publisher"
	log "github.com/Sirupsen/logrus"
	. "github.com/SmartyStreets/goconvey/convey"
)

func Test_init(t *testing.T) {
	Convey("Setting up a TcpRelay", t, func() {
		log.SetLevel(log.FatalLevel)

		natsUrl := "nats://lanval.example.com:4022"
		clusterId := "orlando"
		clientId := "rinaldo"
		subject := "knights-of-charlemagne"

		relay := &TcpRelay{
			NatsUrl:   natsUrl,
			ClusterId: clusterId,
			ClientId:  clientId,
			Subject:   subject,
		}

		Convey("sets up the connection properly", func() {
			relay.init()

			connection, ok := relay.connection.(*publisher.StanPublisher)
			So(ok, ShouldBeTrue)

			So(connection.NatsUrl, ShouldEqual, natsUrl)
			So(connection.ClusterId, ShouldEqual, clusterId)
			So(connection.ClientId, ShouldEqual, clientId)
			So(connection.Subject, ShouldEqual, subject)
			So(connection.Stats, ShouldEqual, stats)
		})
	})
}

type MockConnection struct {
	Filename    string
	initialized bool
	file        *os.File
	MaxRecords  int
	count       int
}

// Sort of mocked stream where we have one record in a file and we
// just re-seek to the beginning on EOF
func (m *MockConnection) Read(p []byte) (n int, err error) {
	if !m.initialized {
		m.file, err = os.Open(m.Filename)
		if err != nil {
			return 0, err
		}
		m.initialized = true

		if m.MaxRecords == 0 {
			m.MaxRecords = 1
		}
	}

	m.count += 1

	// Wrap around to the beginning of the file again
	n, err = m.file.Read(p)
	if err == io.EOF {
		m.file.Seek(0, 0)
	} else {
		return n, err
	}

	// Shut down the stream when we hit the limit
	if m.count > m.MaxRecords {
		return 0, io.EOF
	}

	return n, nil
}

func (m *MockConnection) Close() error {
	return nil
}

func Test_handleConnection(t *testing.T) {
	Convey("Handling a connection", t, func() {
		log.SetLevel(log.FatalLevel)

		natsUrl := "nats://lanval.example.com:4022"
		clusterId := "orlando"
		clientId := "rinaldo"
		subject := "knights-of-charlemagne"

		relay := &TcpRelay{
			NatsUrl:   natsUrl,
			ClusterId: clusterId,
			ClientId:  clientId,
			Subject:   subject,
		}

		Convey("processes valid records from the stream", func() {
			relay.init()
			conn := &MockConnection{
				Filename:   "fixtures/heka.pbuf",
				MaxRecords: 3,
			}
			mockPublisher := &publisher.MockPublisher{Available: true}
			relay.connection = mockPublisher

			relay.handleConnection(conn)

			So(mockPublisher.LastMsg, ShouldNotBeNil)
			So(*mockPublisher.LastMsg.Hostname, ShouldEqual, "ubuntu")
			// TODO we currently lose the last record on EOF!
			So(mockPublisher.RelayedCount, ShouldEqual, 2)
		})
	})
}
