Lägermeister
============

A Heka message compatible logging forwarder backed by NATS. This serves as a
centralized log forwarder so that clients can have a local relay that will
accept large volumes of logs and which is capable of batching them into groups
and forwarding to a logging system like Sumologic.

It uses NATS-streaming to act as the main message buffer and is comprised of
three main parts:

1. **Message receiver**: Currently this requires POSTing individual Heka Protobuf
   messages to the HTTP endpoint. It will also support TCP streaming protocol
   once a suitable implementation is working at reasonable performance.
   **location**: `http_recevier/`

2. **NATS-Streaming Server**: Acts as a centralized buffer and subscription
   manager for log consumers. It will guarantee at-least-once delivery for each
   message. Becaus the server tracks the current offset, and offset in time,
   consumers can start from where they left off and don't need to store state.
   They may also replay starting from a particular point in time.

3. **Subscriber**: A NATS-Streaming subscriber that will pull logs from NATS-
   Streaming and batch them into groups of JSON messages. These will then be
   POSTed to a remote logging service like Sumologic or Loggly. Each consumer
   will run a fixed number of POSTers in parallel, which should be tuned to
   the performance of the system in question. More than one subscriber can be
   run from the same log subject as long as they are in the same subscriber group.
   In this way NATS-Streaming can load balance the subscribers.
   **location**: `subscriber/`

Configuration
-------------

Programs are configured with environment variables. See the source for more
information on available configuration items. Here are some examples:

```
$ SUB_CLIENT_ID=asdf \
  SUB_SUBJECT="lagermeister-test" \
  SUB_DELIVER_LAST=true \
  SUB_DELIVER_ALL=false \
  SUB_REMOTE_URL="https://endpoint1.collection.us2.sumologic.com/receiver/v1/http/ZaVnC4dhaV2Djfx_aJ93Ht013FC51G9_FuWipqPPW5RSxez24iXceWKhPfxlPh-GVEyTX_ZBxrCMwUh-CMuNn8yPdhXxAxVZkEJWFuO7lmcja6wE3V6WOg==" \
  go run subscriber.go
```
