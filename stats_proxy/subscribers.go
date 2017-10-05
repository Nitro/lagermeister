package main

import (
	"encoding/json"

	"github.com/Nitro/lagermeister/event"
	log "github.com/sirupsen/logrus"
)

const (
	CommandChannelSize = 10
	RecencyLength      = 500
)

const (
	CmdSubscribe        = iota
	CmdUnsubscribe      = iota
	CmdUpdateRecent     = iota
	CmdUpdatePublishers = iota
)

type Subscriber struct {
	Name       string
	ListenChan chan []byte
}

func NewSubscriber(name string) *Subscriber {
	return &Subscriber{
		Name: name,
		// Add listener channel. Need to also buffer at least the recent events.
		ListenChan: make(chan []byte, RecencyLength),
	}
}

type Command struct {
	cmdType    int
	subscriber *Subscriber
	event      []byte
	decodedEvt *event.MetricEvent
}

type SubscriptionManager struct {
	subscribers  []*Subscriber
	cmdChan      chan *Command
	quitChan     chan struct{}
	recentEvents [][]byte
	Publishers   map[string]map[string]bool
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		cmdChan:    make(chan *Command, CommandChannelSize),
		quitChan:   make(chan struct{}),
		Publishers: make(map[string]map[string]bool),
	}
}

func (s *SubscriptionManager) Run() {
	go func() {
		for {
			select {
			case <-s.quitChan:
				log.Info("Subscription Manager shutting down")
				return
			case cmd := <-s.cmdChan:
				s.processCommand(cmd)
			}
		}
	}()
}

func (s *SubscriptionManager) Quit() {
	close(s.quitChan)
}

func (s *SubscriptionManager) processCommand(cmd *Command) {
	switch cmd.cmdType {
	case CmdSubscribe:
		// Slurp all the recent history into the channel. Also hook
		// up the subscription afterward.
		for _, event := range s.recentEvents {
			cmd.subscriber.ListenChan <- event
		}
		s.subscribers = append(s.subscribers, cmd.subscriber)
	case CmdUnsubscribe:
		for i, sub := range s.subscribers {
			// If we matched the name, delete it from the slice
			if sub.Name == cmd.subscriber.Name {
				s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
				return
			}

			log.Errorf("Unable to unsubscribe '%s'... possibly leaked subscriber", cmd.subscriber.Name)
		}
	case CmdUpdateRecent:
		if len(s.recentEvents) >= RecencyLength {
			s.recentEvents = append(s.recentEvents[1:RecencyLength], cmd.event)
		} else {
			s.recentEvents = append(s.recentEvents, cmd.event)
		}
	case CmdUpdatePublishers:
		if len(cmd.decodedEvt.MetricType) < 1 {
			log.Warn("Got event with no MetricType!")
			return
		}
		if len(cmd.decodedEvt.Sender) < 1 {
			log.Warn("Got event with no Sender!")
			return
		}
		slot, ok := s.Publishers[cmd.decodedEvt.MetricType]
		if !ok {
			s.Publishers[cmd.decodedEvt.MetricType] = make(map[string]bool)
			slot = s.Publishers[cmd.decodedEvt.MetricType]
		}
		slot[cmd.decodedEvt.Sender] = true
	default:
		log.Errorf("Unexpected command type: %d", cmd.cmdType)
	}
}

func (s *SubscriptionManager) Subscribe(subscriber *Subscriber) {
	s.cmdChan <- &Command{
		cmdType:    CmdSubscribe,
		subscriber: subscriber,
	}
}

func (s *SubscriptionManager) Unsubscribe(subscriber *Subscriber) {
	s.cmdChan <- &Command{
		cmdType:    CmdUnsubscribe,
		subscriber: subscriber,
	}
}

func decodeEvent(evt []byte) (*event.MetricEvent, error) {
	var output event.MetricEvent
	err := json.Unmarshal(evt, &output)
	if err != nil {
		return nil, err
	}

	return &output, nil
}

// Publish will fan a message out to all the subcribers in the list if any
func (s *SubscriptionManager) Publish(event []byte) {
	s.cmdChan <- &Command{
		cmdType: CmdUpdateRecent,
		event:   event,
	}

	// In the background fire off the Json decoding and update for publishers
	go func(event []byte) {
		decoded, err := decodeEvent(event)
		if err != nil {
			log.Warnf("Unable to decode event: %s", err)
			return
		}

		s.cmdChan <- &Command{
			cmdType:    CmdUpdatePublishers,
			decodedEvt: decoded,
		}
	}(event)

	for _, listener := range s.subscribers {
		select {
		case listener.ListenChan <- event:
		default:
			log.Warnf("Unable to publish to subscriber '%s'", listener.Name)
		}
	}
}
