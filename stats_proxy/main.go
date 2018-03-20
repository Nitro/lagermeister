package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kelseyhightower/envconfig"
	"github.com/nats-io/go-nats"
	log "github.com/sirupsen/logrus"
	"gopkg.in/relistan/rubberneck.v1"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var messageChan chan []byte

type Config struct {
	HttpPort   int    `envconfig:"HTTP_PORT" default:"9010"`
	NatsUrl    string `envconfig:"NATS_URL" default:"nats://localhost:4222"`
	SubChannel string `envconfig:"SUB_CHANNEL" default:"stats-events"`
}

func wrapHandler(fn func(w http.ResponseWriter, r *http.Request, mgr *SubscriptionManager), mgr *SubscriptionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fn(w, r, mgr)
	}
}

func configHandler(w http.ResponseWriter, r *http.Request, mgr *SubscriptionManager) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")

	defer r.Body.Close()

	output, err := json.Marshal(mgr.Publishers)
	if err != nil {
		http.Error(w, `{"status": "error", "message": "unable to marshal Publishers"}`, 500)
		return
	}

	w.Write(output)
}

func websockHandler(w http.ResponseWriter, r *http.Request, mgr *SubscriptionManager) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")

	defer r.Body.Close()

	// Construct a unique name for us for the susbcription list
	sub := NewSubscriber(r.RequestURI + "--" + r.RemoteAddr + "-" + time.Now().UTC().String())

	mgr.Subscribe(sub)
	defer mgr.Unsubscribe(sub)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error(err)
		return
	}

	for message := range sub.ListenChan {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Error(err)
			return
		}
	}
}

func serveHttp(config Config, mgr *SubscriptionManager) {
	http.HandleFunc("/messages", wrapHandler(websockHandler, mgr))
	http.HandleFunc("/config", wrapHandler(configHandler, mgr))
	err := http.ListenAndServe(
		fmt.Sprintf(":%d", config.HttpPort), http.DefaultServeMux,
	)

	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	messageChan = make(chan []byte)

	var config Config
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		envconfig.Usage("proxy", &config)
		os.Exit(1)
	}
	err := envconfig.Process("proxy", &config)
	if err != nil {
		log.Fatal(err)
	}
	rubberneck.Print(config)

	nc, err := nats.Connect(config.NatsUrl)
	if err != nil {
		log.Fatalf("Unable to connect to NATS: %s", err)
	}

	// We need something to handle subscriptions from each web request.
	// SubscriptionManager does that for us.
	mgr := NewSubscriptionManager()
	mgr.Run()

	nc.Subscribe(config.SubChannel, func(m *nats.Msg) {
		log.Debugf("Received a message: %s\n", string(m.Data))
		// The manager will fan this out to all the live connections if there are any
		mgr.Publish(m.Data)
	})

	serveHttp(config, mgr)

	mgr.Quit()
}
