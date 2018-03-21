package main

import (
	"net/http"

	"github.com/kelseyhightower/envconfig"
	log "github.com/sirupsen/logrus"
)

type Config struct {
	ListenAddr string `envconfig:"LISTEN_ADDR" default:":3452"`
}

func main() {
	var config Config

	err := envconfig.Process("ui", &config)
	if err != nil {
		log.Fatalf("Unable to start UI server: %s", err)
	}
	http.Handle("/", http.FileServer(http.Dir("./dist")))
	log.Fatal(http.ListenAndServe(config.ListenAddr, nil))
}
