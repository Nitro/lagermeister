package main

import (
	"encoding/json"
	"math/rand"
	"time"

	"github.com/Nitro/lagermeister/event"
	"github.com/nats-io/go-nats"
	log "github.com/sirupsen/logrus"
)

var IPs = []string{
	"10.10.10.1",
	"10.12.12.1",
	"10.12.10.10",
}

func random(min, max int) int {
	rand.Seed(time.Now().Unix())
	return rand.Intn(max-min) + min
}

func publishBatchCount(publisherChan chan *event.MetricEvent) {
	for {
		evt := &event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			SourceIP:   IPs[random(0, 3)],
			Sender:     "sumologic-publisher",
			MetricType: "BatchCount",
			Value:      float64(random(50, 90)),
		}

		publisherChan <- evt
		time.Sleep(1 * time.Second)
	}
}

func publishLag(publisherChan chan *event.MetricEvent) {
	thresholds := make(map[string]float64)
	thresholds["Warn"] = 60.0
	thresholds["Error"] = 120.0

	for {

		evt := &event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			Value:      float64(random(0, 50)),
			SourceIP:   IPs[random(0, 3)],
			Sender:     "sumologic-publisher",
			MetricType: "Lag",
			Threshold:  thresholds,
		}

		publisherChan <- evt
		time.Sleep(1 * time.Second)
	}
}

func publishThroughput(publisherChan chan *event.MetricEvent) {
	for {
		httpEvt := &event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			Value:      float64(random(500, 2048)),
			SourceIP:   IPs[random(0, 3)],
			Sender:     "http-receiver",
			MetricType: "Throughput",
		}

		tcpEvt := &event.MetricEvent{
			Timestamp:  time.Now().UTC().Unix(),
			Value:      float64(random(100, 755)),
			SourceIP:   IPs[random(0, 3)],
			Sender:     "tcp-receiver",
			MetricType: "Throughput",
		}

		publisherChan <- httpEvt
		publisherChan <- tcpEvt
		time.Sleep(1 * time.Second)
	}
}

func publisher(publishChan chan *event.MetricEvent) {
	nc, _ := nats.Connect(nats.DefaultURL)

	for evt := range publishChan {
		data, err := json.Marshal(*evt)
		if err != nil {
			log.Warnf("Unable to JSON marshal event: %s", err)
			continue
		}

		err = nc.Publish("stats-events", data)
		if err != nil {
			log.Warnf("Unable to publish event: %s", err)
			continue
		}
	}

	nc.Close()
}

func main() {
	publisherChan := make(chan *event.MetricEvent, 100)

	go publisher(publisherChan)
	go publishLag(publisherChan)
	go publishThroughput(publisherChan)
	go publishBatchCount(publisherChan)

	select {}
}
