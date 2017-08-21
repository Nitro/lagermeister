package main

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"github.com/Nitro/lagermeister/message"
	log "github.com/Sirupsen/logrus"
	. "github.com/SmartyStreets/goconvey/convey"
)

type MockPublisher struct {
	available        bool
	connectWasCalled bool
}

func (m *MockPublisher) Connect() error {
	m.connectWasCalled = true
	return nil
}

func (m *MockPublisher) BreakerOn() {
	m.available = false
}

func (m *MockPublisher) BreakerOff() {
	m.available = true
}

func (m *MockPublisher) IsAvailable() bool {
	return m.available
}

func (m *MockPublisher) RelayMessage(*message.Message) {
}

func Test_HealthCheck(t *testing.T) {
	log.SetLevel(log.FatalLevel)

	Convey("The health check", t, func() {
		req := httptest.NewRequest("GET", "http://chaucer.example.com/health", nil)
		w := httptest.NewRecorder()

		relay := &HttpRelay{}
		relay.init()

		Convey("Identifies when the circuit breaker is thrown", func() {
			// Here we exercise the real code
			relay.connection.BreakerOn()
			relay.handleHealth(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 502)
			So(string(body), ShouldContainSubstring, "breaker is flipped")
		})

		Convey("Returns variables when healthy", func() {
			// Here we exercise the real code
			relay.connection.BreakerOff()
			relay.handleHealth(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 200)
			So(string(body), ShouldContainSubstring, "{}")
		})
	})
}

type ErroringReader struct{}

func (e *ErroringReader) Read(data []byte) (int, error) {
	return 0, errors.New("Intentional error!")
}

func Test_HandleReceive(t *testing.T) {
	Convey("Receiving messages on the wire", t, func() {
		req := httptest.NewRequest("POST", "http://chaucer.example.com/health", nil)
		w := httptest.NewRecorder()

		mockConnection := &MockPublisher{available: true}
		relay := &HttpRelay{connection: mockConnection}
		relay.init()

		Reset(func() {
			stats.Init()
		})

		Convey("returns an error when the breaker is on", func() {
			mockConnection.available = false
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 502)
			So(string(body), ShouldContainSubstring, "Invalid NATS")
			So(stats.Get("refusedCount").String(), ShouldEqual, "1")
		})

		Convey("tries to reconnect when the breaker is on", func() {
			mockConnection.available = false
			relay.handleReceive(w, req)

			So(mockConnection.connectWasCalled, ShouldBeTrue)
		})

		Convey("captures errors when the socket has errors", func() {
			req := httptest.NewRequest("POST", "http://chaucer.example.com/health", &ErroringReader{})
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 500)
			So(string(body), ShouldContainSubstring, "Socket read error")
		})

		Convey("captures errors when the protobuf is malformed", func() {
			buf := bytes.NewReader([]byte(`busted junk!`))
			req := httptest.NewRequest("POST", "http://chaucer.example.com/health", buf)
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 400)
			So(string(body), ShouldContainSubstring, "Invalid message")
			So(stats.Get("errorCount").String(), ShouldEqual, "1")
		})

		Convey("happily moves along when some required fields are missing", func() {
			buf := bytes.NewReader([]byte{})
			req := httptest.NewRequest("POST", "http://chaucer.example.com/health", buf)
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 200)
			So(string(body), ShouldBeEmpty)
			So(stats.Get("skipped").String(), ShouldEqual, "1")
		})
	})
}
