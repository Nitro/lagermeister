package event

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/nats-io/go-nats"
	"github.com/pquerna/ffjson/ffjson"
)

const (
	StatsChanBufSize   = 4096             // We buffer this many stats/sec
)

type MetricReporter struct {
	statsChan chan *MetricEvent
	statsConn *nats.Conn
	quitChan  chan struct{}
}

func NewMetricReporter() *MetricReporter {
	return &MetricReporter{
		statsChan: make(chan *MetricEvent, StatsChanBufSize),
		quitChan: make(chan struct{}),
	}
}

func (r *MetricReporter) TrySendMetrics(evt *MetricEvent) {
	select {
	case r.statsChan <- evt:
		// Great! Nothing else to do
	default:
		log.Warn("Unable to enqueue stats event, channel full!")
	}
}

func (r *MetricReporter) ProcessMetrics() error {
	var err error

	// TODO get these from the config!
	r.statsConn, err = nats.Connect("nats://localhost:4222")
	if err != nil {
		return fmt.Errorf("Unable to connect to NATS: %s", err)
	}

	go func() {
		ticker := time.NewTicker(time.Second)
		for {
			// Check the quit channel, or wait for the ticker
			select {
			case <-r.quitChan:
				log.Info("Shutting down metric reporter")
				return
			case <-ticker.C:
				// continue on below
			}

			evtCount := len(r.statsChan)
			accumulator := make(map[string]float64)
			metrics := make(map[string]*MetricEvent)
			counts := make(map[string]int64)
			var metric *MetricEvent

			if evtCount < 1 {
				continue // No events, wait for more
			}

			// Aggregate the values into a big number
			for i := 0; i < evtCount; i++ {
				metric = <-r.statsChan
				accumulator[metric.MetricType] += metric.Value
				metrics[metric.MetricType] = metric
				counts[metric.MetricType] += 1
			}

			// Take the average/total of the big number, use timestamp and
			// remaining values from the LAST event.
			for name, metric := range metrics {
				if counts[name] < 1 {
					continue
				}

				switch metric.Aggregate {
				case "Average":
					metric.Value = accumulator[name] / float64(counts[name])
				case "Total":
					metric.Value = accumulator[name]
				}

				buf, _ := ffjson.Marshal(metric)
				err = r.statsConn.Publish("stats-events", buf)
				ffjson.Pool(buf)
				if err != nil {
					log.Warnf("Unable to publich stats event: %s", err)
				}
			}
		}
	}()

	return nil
}

func (r *MetricReporter) Quit() {
	close(r.quitChan)
	r.quitChan = nil
}
