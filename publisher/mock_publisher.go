package publisher

import "github.com/Nitro/lagermeister/message"

type MockPublisher struct {
	Available        bool
	ConnectWasCalled bool
	LastMsg          *message.Message
	RelayedCount     int
}

func (m *MockPublisher) Connect() error {
	m.ConnectWasCalled = true
	return nil
}

func (m *MockPublisher) BreakerOn() {
	m.Available = false
}

func (m *MockPublisher) BreakerOff() {
	m.Available = true
}

func (m *MockPublisher) IsAvailable() bool {
	return m.Available
}

func (m *MockPublisher) RelayMessage(msg *message.Message) {
	m.RelayedCount += 1
	m.LastMsg = msg
}

func (m *MockPublisher) Shutdown() {}
