package main

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Nitro/lagermeister/message"
	"github.com/Nitro/lagermeister/publisher"
	log "github.com/sirupsen/logrus"
	. "github.com/smartystreets/goconvey/convey"
)

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

		mockConnection := &publisher.MockPublisher{Available: true}
		relay := &HttpRelay{connection: mockConnection}
		relay.init()

		Reset(func() {
			stats.Init()
		})

		Convey("returns an error when the breaker is on", func() {
			mockConnection.Available = false
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 502)
			So(string(body), ShouldContainSubstring, "Invalid NATS")
			So(stats.Get("refusedCount").String(), ShouldEqual, "1")
		})

		Convey("tries to reconnect when the breaker is on", func() {
			mockConnection.Available = false
			relay.handleReceive(w, req)

			So(mockConnection.ConnectWasCalled, ShouldBeTrue)
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
			So(stats.Get("errorCount"), ShouldNotBeNil)
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
			So(stats.Get("skipped"), ShouldNotBeNil)
			So(stats.Get("skipped").String(), ShouldEqual, "1")
		})

		Convey("relays all messages when the matcher is empty", func() {
			file, _ := os.Open("fixtures/heka.pbuf")
			req := httptest.NewRequest("POST", "http://chaucer.example.com/health", file)
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 200)
			So(string(body), ShouldBeEmpty)
			So(stats.Get("skipped"), ShouldBeNil)
			So(mockConnection.LastMsg, ShouldNotBeNil)
			So(*mockConnection.LastMsg.Hostname, ShouldEqual, "ubuntu")
		})

		Convey("relays messages that match the matcher", func() {
			file, _ := os.Open("fixtures/heka.pbuf")
			req := httptest.NewRequest("POST", "http://chaucer.example.com/health", file)
			var err error
			relay.matcher, err = message.CreateMatcherSpecification("Hostname == 'ubuntu'")
			So(err, ShouldBeNil)
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 200)
			So(string(body), ShouldBeEmpty)
			So(mockConnection.LastMsg, ShouldNotBeNil)
			So(*mockConnection.LastMsg.Hostname, ShouldEqual, "ubuntu")
		})

		Convey("does not relay non-matching messages", func() {
			relay.MatchSpec = "Hostname == 'something-else'"
			relay.init()

			file, _ := os.Open("fixtures/heka.pbuf")
			req := httptest.NewRequest("POST", "http://chaucer.example.com/health", file)
			var err error
			So(err, ShouldBeNil)
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 200)
			So(string(body), ShouldBeEmpty)
			So(mockConnection.LastMsg, ShouldBeNil)
		})

		Convey("refuses anything but POST messages", func() {
			req := httptest.NewRequest("GET", "http://chaucer.example.com/health", nil)
			relay.handleReceive(w, req)

			resp := w.Result()
			body, _ := ioutil.ReadAll(resp.Body)

			So(resp.StatusCode, ShouldEqual, 405)
			So(string(body), ShouldContainSubstring, "Expected POST")
		})
	})
}
