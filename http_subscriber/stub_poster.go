package main

// This implements an Http poster that uses a stubbed HTTP Transport
// which will respond with a reasonable range of response times.

import (
	"math/rand"
	"net/http"
	"time"

	"gopkg.in/jarcoal/httpmock.v1"
	log "github.com/Sirupsen/logrus"
)

const (
	MinStubbedResponseTime = 50  // Milliseconds
	MaxStubbedResponseTime = 700 // Milliseconds
)

// random returns a pseudo-random integer between min and max
func random(min, max int) int {
	rand.Seed(time.Now().Unix())

	return rand.Intn(max-min) + min
}

// randomlySlowResponder responds with a valid http response but takes a random
// amount of time to respond, somewhere between MinStubbedResponseTime and
// MaxStubbedResponseTime
func randomlySlowResponder(*http.Request) (*http.Response, error) {
	body := struct{ status string }{"ok"}
	resp, _ := httpmock.NewJsonResponse(200, body)

	sleepDuration := time.Duration(random(MinStubbedResponseTime, MaxStubbedResponseTime)) * time.Millisecond
	log.Warn("Sleep duration: %s", sleepDuration)
	time.Sleep(sleepDuration)

	return resp, nil
}

// setupMockEndpoint creates a mocked transport using the randomlySlowResponder
// and then returns that transport for use in an HTTP Client.
func setupMockEndpoint(url string) http.RoundTripper {
	transport := httpmock.NewMockTransport()
	transport.RegisterResponder("POST", url, randomlySlowResponder)

	return transport
}

// NewStubHttpMessagePoster returns an HttpMessagePoster where the HTTP Client
// uses a stubbed transport layer where the response times are as per the
// randomlySlowResponder (i.e between MinStubbedResponseTime and
// MaxStubbedResponseTime)
func NewStubHttpMessagePoster(url string, timeout time.Duration) *HttpMessagePoster {
	transport := setupMockEndpoint(url)

	poster := NewHttpMessagePoster(url, timeout)
	poster.client.Transport = transport

	httpmock.Activate()

	return poster
}
