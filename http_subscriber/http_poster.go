package main

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/Nitro/lagermeister/event"
	log "github.com/Sirupsen/logrus"
	"github.com/nats-io/go-nats"
	"github.com/pquerna/ffjson/ffjson"
)

type HttpMessagePoster struct {
	Url     string
	Timeout time.Duration

	client     *http.Client
	semaphores chan struct{}
	statsChan  chan *event.MetricEvent
	statsConn  *nats.Conn
}

func NewHttpMessagePoster(url string, timeout time.Duration) *HttpMessagePoster {
	poster := &HttpMessagePoster{
		Url:     url,
		Timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
		},
		semaphores: make(chan struct{}, PosterPoolSize),
		statsChan:  make(chan *event.MetricEvent, StatsChanBufSize),
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
		log.Warn("All semaphores in use!")
	}

	go func() {
		startTime := time.Now()
		defer func() {
			log.Debugf("POST time taken: %s", time.Now().Sub(startTime))
		}()

		resp, err := h.client.Post(h.Url, "application/json", buf)
		h.semaphores <- semaphore // This should not block if implemented properly
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
		h.TrySendStats(&event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			Value:      float64(BatchSize),
			Sender:     "http-output", // TODO make this configurable
			MetricType: "BatchSize",
		})

		h.TrySendStats(&event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			Value:      float64(BatchSize),
			Sender:     "http-output", // TODO make this configurable
			MetricType: "Throughput",
		})
	}()
}

func (h *HttpMessagePoster) TrySendStats(evt *event.MetricEvent) {
	select {
	case h.statsChan <- evt:
		// Great! Nothing else to do
	default:
		log.Warn("Unable to enqueue stats event, channel full!")
	}
}

func (h *HttpMessagePoster) ProcessStats() error {
	var err error

	// TODO get these from the config!
	h.statsConn, err = nats.Connect("nats://localhost:4222")
	if err != nil {
		return fmt.Errorf("Unable to connect to NATS: %s", err)
	}

	go func() { // Will exit on channel close
		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			evtCount := len(h.statsChan)
			aggregator := make(map[string]float64)
			metrics := make(map[string]*event.MetricEvent)
			counts := make(map[string]int64)
			var metric *event.MetricEvent

			if evtCount < 1 {
				continue // No events, wait for more
			}

			// Aggregate the values into a big number
			for i := 0; i < evtCount; i++ {
				metric = <-h.statsChan
				aggregator[metric.MetricType] += metric.Value
				metrics[metric.MetricType] = metric
				counts[metric.MetricType] += 1
			}

			// Take the average of the big number, use timestamp and
			// remaining values from the LAST event.
			if counts["BatchSize"] > 0 {
				metrics["BatchSize"].Value = aggregator["BatchSize"] / float64(counts["BatchSize"])
				buf, _ := ffjson.Marshal(metrics["BatchSize"])
				err = h.statsConn.Publish("stats-events", buf)
				ffjson.Pool(buf)
			}

			if counts["Throughput"] > 0 {
				metrics["Throughput"].Value = aggregator["Throughput"]
				buf, _ := ffjson.Marshal(metrics["Throughput"])
				err = h.statsConn.Publish("stats-events", buf)
				ffjson.Pool(buf)
			}

			if counts["Lag"] > 0 {
				metrics["Lag"].Value = aggregator["Lag"] / float64(counts["Lag"])
				buf, _ := ffjson.Marshal(metrics["Lag"])
				err = h.statsConn.Publish("stats-events", buf)
				ffjson.Pool(buf)
			}

			if err != nil {
				log.Warnf("Unable to publich stats event: %s", err)
			}
		}
	}()

	return nil
}
