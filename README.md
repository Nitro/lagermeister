Lägermeister
============

Lägermeister is a Heka message compatible logging forwarder backed by NATS.
This serves as a centralized log forwarder so that clients can have a local
relay that will accept large volumes of logs and which is capable of batching
them into groups and forwarding to a logging system like Sumologic.

It is comprised of a number of smaller programs that are all tied together
around a shared message broker. Each program has its own configuration and runs
in its own process space. The message broker interface is the way that
components talk to each other and it also acts as a buffer between components
of the system. In this way temporary burst load can be absorbed by the broker
and components can be scaled up and down individually based on the needs of the
pipeline they serve.

Lägermeister uses NATS-streaming as the message broker. The system is comprised
of three main parts:

1. **Message receiver**: There are two implementations of the message receiver:
   an HTTP receiver and a TCP streaming receiver that processes HekaFraming
   messages. Both receiver expect Heka protobuf messages. A single stream can
   push about 5500 3KB messages per second currently. The HTTP receiver should
   be able to handle substantially more, though with a much larger network
   footprint. Each streaming receiver should be able to handle thousands of
   open connections.
   **location**: `http_recevier/`, `tcp_receiver/`

2. **NATS-Streaming Server**: Acts as a centralized buffer and subscription
   manager for log consumers. It will guarantee at-least-once delivery for each
   message. Becaus the server tracks the current offset, and offset in time,
   consumers can start from where they left off and don't need to store state.
   They may also replay starting from a particular point in time.
   **location**: external service!

3. **Subscriber**: A NATS-Streaming subscriber that will pull logs from NATS-
   Streaming and batch them into groups of JSON messages. These will then be
   POSTed to a remote logging service like Sumologic or Loggly. Each consumer
   will run a fixed number of POSTers in parallel, which should be tuned to
   the performance of the system in question. More than one subscriber can be
   run from the same log subject as long as they are in the same subscriber group.
   In this way NATS-Streaming can load balance the subscribers.
   **location**: `http_subscriber/`

Components
----------

These are the components that currently make up Lagermeister, and the
environment variables that are required to configure them.

### `http_receiver`

Receives incoming Protobuf messages over HTTP, validates them and stuffs them
into a NATS streaming channel.

```
KEY                 TYPE      DEFAULT                  REQUIRED    DESCRIPTION
SUB_BIND_ADDRESS    String    :35001
SUB_NATS_URL        String    nats://localhost:4222
SUB_CLUSTER_ID      String    test-cluster
SUB_CLIENT_ID       String                             true
SUB_SUBJECT         String    lagermeister-test
SUB_MATCH_SPEC      String
```

### `http_subscriber`

Listens on a NATS streaming channel and batches and sends groups of messages in
JSON format to a defined HTTP endpoint.

```
KEY                  TYPE                DEFAULT                  REQUIRED    DESCRIPTION
SUB_CLUSTER_ID       String              test-cluster
SUB_CLIENT_ID        String
SUB_START_SEQ        Unsigned Integer
SUB_START_DELTA      String
SUB_DELIVER_ALL      True or False
SUB_DELIVER_LAST     True or False
SUB_DURABLE          String
SUB_QGROUP           String
SUB_UNSUBSCRIBE      True or False
SUB_NATS_URL         String              nats://localhost:4222
SUB_SUBJECT          String
SUB_STUB_HTTP        True or False       false
SUB_REMOTE_URL       String                                       true
SUB_LOGGING_LEVEL    String              info
SUB_BATCH_TIMEOUT    Duration            10s
```

An example of configuring this component would look something like this:
```
$ SUB_CLIENT_ID=asdf \
  SUB_SUBJECT="lagermeister-test" \
  SUB_DELIVER_LAST=true \
  SUB_DELIVER_ALL=false \
  SUB_REMOTE_URL="https://endpoint1.collection.us2.sumologic.com/receiver/v1/http/ZaVnC4dhaV2Djfx_aJ93Ht013FC51G9_FuWipqPPW5RSxez24iXceWKhPfxlPh-GVEyTX_ZBxrCMwUh-CMuNn8yPdhXxAxVZkEJWFuO7lmcja6wE3V6WOg==" \
  go run subscriber.go
```


### `log_generator`

Generates messages and sends them to a local instance of the `http_receiver`.
Currently takes no configuration.

### `stats_proxy`

Used by the web monitoring frontend to gather statistics from a NATS pub/sub
channel and relay them over a websocket.

```
KEY                TYPE       DEFAULT                  REQUIRED    DESCRIPTION
SUB_HTTP_PORT      Integer    9010
SUB_NATS_URL       String     nats://localhost:4222
SUB_SUB_CHANNEL    String     stats-events
```

### `tcp_receiver`

A receiver that listens on TCP and takes Heka-framed streaming TCP messages
and relays them into a NATS streaming channel.

```
KEY                      TYPE             DEFAULT                  REQUIRED    DESCRIPTION
SUB_BIND_ADDRESS         String           :35000
SUB_NATS_URL             String           nats://localhost:4222
SUB_CLUSTER_ID           String           test-cluster
SUB_CLIENT_ID            String                                    true
SUB_SUBJECT              String           lagermeister-test
SUB_MATCH_SPEC           String
SUB_STATS_ADDRESS        String           :34999
SUB_LISTEN_COUNT         Integer          20
SUB_LOGGING_LEVEL        String           info
SUB_KEEPALIVE            True or False
SUB_KEEPALIVEDURATION    Duration
```
