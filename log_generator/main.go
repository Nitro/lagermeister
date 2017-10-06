package main

import (
	"bytes"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Nitro/lagermeister/message"
	"github.com/gogo/protobuf/proto"
)

const (
	LagermeisterUrl = "http://localhost:35001/"
)

var count int64

func main() {
	for i := 0; i < 2; i++ {
		go func() {
			for {
				now := time.Now().Unix() * 1e9
				payload := `This is the awesomest message`
				var pid int32 = 1234
				hostname := "nitro-test-generator"
				raw := &message.Message{
					Uuid:      []byte(`asdfasdf-asf-sad-fasdf-asdf`),
					Timestamp: &now,
					Payload:   &payload,
					Pid:       &pid,
					Hostname:  &hostname,
				}

				message, err := proto.Marshal(raw)
				if err != nil {
					println("Error marshaling: ", err.Error())
				}

				buf := bytes.NewBuffer(message)
				_, err = http.Post(LagermeisterUrl, "application/octet-stream", buf)
				buf.Reset()

				if err != nil {
					println("Error sendin on http socket:", err.Error())
				}

				atomic.AddInt64(&count, 1)
			}
		}()
	}

	for {
		println("Messages sent: ", count)
		time.Sleep(1 * time.Second)
	}
}
