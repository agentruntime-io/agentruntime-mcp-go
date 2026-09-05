package relay

import (
	"encoding/json"
	"sync"
	"time"
)

type pendingRequest struct {
	ch      chan json.RawMessage
	created time.Time
}

// RequestMultiplexer correlates JSON-RPC-style id fields on one socket.
type RequestMultiplexer struct {
	mu      sync.Mutex
	pending map[string]chan json.RawMessage
	timeout time.Duration
}

func NewRequestMultiplexer(timeout time.Duration) *RequestMultiplexer {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &RequestMultiplexer{
		pending: make(map[string]chan json.RawMessage),
		timeout: timeout,
	}
}

func (m *RequestMultiplexer) Register(id string) <-chan json.RawMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan json.RawMessage, 1)
	m.pending[id] = ch
	return ch
}

func (m *RequestMultiplexer) Complete(id string, body json.RawMessage) bool {
	m.mu.Lock()
	ch, ok := m.pending[id]
	if ok {
		delete(m.pending, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- body:
	default:
	}
	close(ch)
	return true
}

func (m *RequestMultiplexer) Wait(ch <-chan json.RawMessage) (json.RawMessage, bool) {
	select {
	case body, ok := <-ch:
		return body, ok
	case <-time.After(m.timeout):
		return nil, false
	}
}

func (m *RequestMultiplexer) Cancel(id string) {
	m.mu.Lock()
	ch, ok := m.pending[id]
	if ok {
		delete(m.pending, id)
	}
	m.mu.Unlock()
	if ok {
		close(ch)
	}
}
