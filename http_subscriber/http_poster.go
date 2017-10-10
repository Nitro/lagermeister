package main

import (
	"bytes"
	"net/http"
	"time"

	"github.com/Nitro/lagermeister/event"
	log "github.com/Sirupsen/logrus"
)

type HttpMessagePoster struct {
	Url     string
	Timeout time.Duration

	client         *http.Client
	semaphores     chan struct{}
	MetricReporter *event.MetricReporter
}

func NewHttpMessagePoster(url string, timeout time.Duration) *HttpMessagePoster {
	poster := &HttpMessagePoster{
		Url:     url,
		Timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
		semaphores: make(chan struct{}, PosterPoolSize),
	}

	for i := 0; i < PosterPoolSize; i++ {
		poster.semaphores <- struct{}{}
	}

	return poster
}

// Post is potentially blocking since it checks out a semaphore from the semaphore
// pool. If the pool is maxxed out, the call will block until a semaphore is
// returned to the pool.
func (h *HttpMessagePoster) Post(data []byte) {
	if h.client == nil {
		log.Error("Client has not been initialized!")
		return
	}

	// Post requires a Reader so we make one from the byte slice
	buf := bytes.NewBuffer(data)

	// Check out a semaphore so we don't have too many at once
	log.Debugf("Free semaphores: %d/%d", len(h.semaphores), PosterPoolSize)
	semaphore := <-h.semaphores
	if len(h.semaphores) < 1 {
		log.Debug("All semaphores in use!")
	}

	go func() {
		startTime := time.Now()
		defer func() {
			log.Debugf("POST time taken: %s", time.Now().Sub(startTime))
		}()

		resp, err := h.client.Post(h.Url, "application/json", buf)
		h.semaphores <- semaphore // This should not block if implemented properly
		log.Debugf("Semaphore wait time: %s", time.Now().Sub(startTime))
		if err != nil || resp.StatusCode < 200 || resp.StatusCode > 299 {
			status := -1

			if resp != nil {
				status = resp.StatusCode
			}

			log.Errorf(
				"Error posting to %s. Status %d. Error: %s", h.Url, status, err,
			)
		}

		// TODO read this and log response on error
		if resp != nil && resp.Body != nil {
			log.Debugf("Response code: %d", resp.StatusCode)
			resp.Body.Close()
		}

		// Try to send a metric on the stats channel
		if h.MetricReporter != nil {
			h.MetricReporter.TrySendMetrics(&event.MetricEvent{
				Timestamp:  time.Now().UTC().Unix(),
				Value:      float64(BatchSize),
				Aggregate:  "Average",
				Sender:     "http-output", // TODO make this configurable
				MetricType: "BatchSize",
			})

			h.MetricReporter.TrySendMetrics(&event.MetricEvent{
				Timestamp:  time.Now().UTC().Unix(),
				Value:      float64(BatchSize),
				Aggregate:  "Total",
				Sender:     "http-output", // TODO make this configurable
				MetricType: "Throughput",
			})
		}
	}()
}
