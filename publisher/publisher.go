// Package publisher contains a NATS publisher implementation build around
// the lower level NATS code.
package publisher

import (
	"errors"
	"expvar"
	"fmt"
	"sync"
	"time"

	"github.com/Nitro/lagermeister/message"
	"github.com/nats-io/go-nats-streaming"
	log "github.com/sirupsen/logrus"
)

var (
	publishRetries = [...]int{250, 500, 1000, 3000, 5000} // Milliseconds
)

// A Publisher is an outlet for a Heka message that supports a circuit
// breaker and connection management.
type Publisher interface {
	Connect() error
	BreakerOn()
	BreakerOff()
	IsAvailable() bool
	RelayMessage(*message.Message)
	Shutdown()
}

// A StanPublisher is a NATS Publisher with connection management,
// retries, and a circuit breaker that can be flipped while a connection
// can't be established.
type StanPublisher struct {
	NatsUrl         string
	ClusterId       string
	ClientId        string
	Subject         string
	Stats           *expvar.Map
	ConnectHoldDown time.Duration

	stanConn        stan.Conn
	connectSem      chan struct{}
	connectWaitChan chan struct{}
	lastConnect     time.Time
	available       bool
	availLock       sync.RWMutex
}

// Connect is the main method, used to connect the StanPublisher to the
// specified NATS stream.
func (s *StanPublisher) Connect() error {
	// A semaphore to make sure we don't have multiple goroutines trying
	// to reconnect at the same time later on.
	if s.connectSem == nil {
		s.connectSem = make(chan struct{}, 1)
		s.connectSem <- struct{}{} // Make sure there is one semaphore
	}

	if s.connectWaitChan == nil {
		s.connectWaitChan = make(chan struct{})
	}

	err := s.connectStan()
	if err != nil {
		return err // Already annotated in the connectStan function
	}

	return nil
}

// connectStan connects to the NATS streaming cluster
func (s *StanPublisher) connectStan() error {
	var err error

	// Semaphore protected so we don't repeatedly connect at the same time
	select {
	case semaphore := <-s.connectSem:
		defer func() { s.connectSem <- semaphore }() // Release the semaphore when we're done

		if time.Now().UTC().Sub(s.lastConnect) < s.ConnectHoldDown {
			return errors.New("Too soon to reconnect to NATS, punting")
		}

		// Free any pre-existing connection
		if !s.lastConnect.IsZero() && (s.stanConn != nil) {
			s.stanConn.NatsConn().Close()
			s.stanConn.Close()
			s.Stats.Add("publisherReconnectCount", 1)
		}

		s.lastConnect = time.Now().UTC()

		log.Infof("Attempting to connect to NATS streaming: %s clusterID=[%s] clientID=[%s]",
			s.NatsUrl, s.ClusterId, s.ClientId,
		)

		s.stanConn, err = stan.Connect(
			s.ClusterId, s.ClientId, stan.NatsURL(s.NatsUrl),
			stan.ConnectWait(5*time.Second),
			stan.PubAckWait(3*time.Second),
		)
		if err != nil {
			// A ton of failures seem to derive from the Cluster ID not matching on
			// connect. The error reported up from the stan package is not very
			// helpful.
			return fmt.Errorf("Error connecting to NATS streaming: %s", err)
		}

		s.BreakerOff()
		// Notify all waiting goroutines that we are now connected
		close(s.connectWaitChan)
		s.connectWaitChan = make(chan struct{})

		log.Info("Connected to NATS streaming successfully")
	default:
		<-s.connectWaitChan
	}

	return nil
}

// RelayMessage publishes a message to NATS streaming. It is blocking and can
// hold onto the goroutine for several seconds so it should be run only where
// that won't cause any performance issues.
func (s *StanPublisher) RelayMessage(msg *message.Message) {
	data, err := msg.Marshal()
	if err != nil {
		log.Errorf("Encoding: %s", err)
	}

	for i, sleepTime := range publishRetries {
		// Keep this which we'll hold on to only if it was not nil
		conn := s.stanConn

		// Check if we need to reconnect. If so, then we sleep and loop back
		if conn == nil {
			log.Warn("NATS connection was nil. Retrying.")
			time.Sleep(time.Duration(sleepTime) * time.Millisecond)
			s.BreakerOn()
			s.connectStan()
			continue
		}

		err = conn.Publish(s.Subject, data)
		// If nothing went wrong, we're good to go
		if err == nil {
			s.Stats.Add("publishedCount", 1)
			break
		}

		if err == stan.ErrConnectionClosed || err == stan.ErrBadConnection ||
			err == stan.ErrTimeout {

			s.Stats.Add("retryCount", 1)
			log.Warnf("Retrying #%d publishing to NATS, got %s", i, err)
			time.Sleep(time.Duration(sleepTime) * time.Millisecond)
			s.BreakerOn()
			s.connectStan()
			continue
		}

		log.Errorf("Publishing: %s", err)
		s.Stats.Add("errorCount", 1)
	}
}

// first message that has an issue because we don't actively manage the NATS
// connection.
func (s *StanPublisher) BreakerOn() {
	s.availLock.Lock()
	defer s.availLock.Unlock()

	// If we're already flipped, let's not announce it again
	if !s.available {
		return
	}

	s.available = false
	log.Warn("Turning circuit breaker on!")
}

// BreakerOff flips the circuit breaker to off so that we can process any new
// incoming messages.
func (s *StanPublisher) BreakerOff() {
	s.availLock.Lock()
	s.available = true
	log.Warn("Turning circuit breaker off!")
	s.availLock.Unlock()
}

// IsAvailable is used to see if the circuit breaker has been flipped off.
// This is used by consuming code that needs to know if the StanPublisher
// is ready to receive a new message or not, without waiting for a timeout.
func (s *StanPublisher) IsAvailable() bool {
	s.availLock.RLock()
	defer s.availLock.RUnlock()

	return s.available
}

// Shutdown will clean up after the publisher and close open connections
func (s *StanPublisher) Shutdown() {
	s.stanConn.Close()
}
