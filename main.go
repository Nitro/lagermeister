package main

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/nats-io/go-nats-streaming"
	"gopkg.in/relistan/rubberneck.v1"
)

type Config struct {
	ClusterId string `envconfig:"CLUSTER_ID" default:"test-cluster"`
	ClientId  string `envconfig:"CLIENT_ID" default:"lager-1"`
	Url       string `envconfig:"URL" default:"nats://localhost:4222"`
}

func main() {
	var config Config
	envconfig.Process("lagermeister", &config)
	rubberneck.Print(config)

	sc, err := stan.Connect(config.ClusterId, config.ClientId, stan.NatsURL(config.Url), stan.ConnectWait(5*time.Second))
	if err != nil {
		panic(err)
	}

	// Simple Synchronous Publisher
	sc.Publish("foo", []byte("Hello World")) // does not return until an ack has been received from NATS Streaming
	println("Acked!")

	// Simple Async Subscriber
	sub, err := sc.Subscribe("foo", func(m *stan.Msg) {
		fmt.Printf("Received a message: %s\n", string(m.Data))
	})

	if err != nil {
		panic(err)
	}

	// Unsubscribe
	sub.Unsubscribe()

	// Close connection
	sc.Close()
}
