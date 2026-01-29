// Copyright 2024 The Kube-burner Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dashboard

import (
	"context"
	"sync"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const (
	// MaxClients is the maximum number of concurrent WebSocket connections
	MaxClients = 100
	// ClientBufferSize is the size of the message buffer per client
	ClientBufferSize = 256
)

// Broadcaster manages WebSocket connections and broadcasts messages to all clients
type Broadcaster struct {
	clients    map[*websocket.Conn]chan Message
	broadcast  chan Message
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

// NewBroadcaster creates a new broadcaster instance
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients:    make(map[*websocket.Conn]chan Message),
		broadcast:  make(chan Message, ClientBufferSize),
		register:   make(chan *websocket.Conn, 10),
		unregister: make(chan *websocket.Conn, 10),
	}
}

// Run starts the broadcaster event loop
func (b *Broadcaster) Run(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard broadcaster panic recovered: %v", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Debug("Dashboard broadcaster shutting down")
			b.mu.Lock()
			for conn, ch := range b.clients {
				close(ch)
				conn.Close()
				delete(b.clients, conn)
			}
			b.mu.Unlock()
			return

		case conn := <-b.register:
			b.mu.Lock()
			if len(b.clients) >= MaxClients {
				b.mu.Unlock()
				log.Warnf("Dashboard max clients (%d) reached, rejecting new connection", MaxClients)
				conn.Close()
				continue
			}
			b.clients[conn] = make(chan Message, ClientBufferSize)
			b.mu.Unlock()
			log.Debugf("Dashboard client connected, total clients: %d", len(b.clients))

			// Start goroutine to write messages to this client
			go b.writeToClient(ctx, conn)

		case conn := <-b.unregister:
			b.mu.Lock()
			if ch, ok := b.clients[conn]; ok {
				close(ch)
				delete(b.clients, conn)
				conn.Close()
				log.Debugf("Dashboard client disconnected, total clients: %d", len(b.clients))
			}
			b.mu.Unlock()

		case message := <-b.broadcast:
			b.mu.RLock()
			for _, ch := range b.clients {
				select {
				case ch <- message:
					// Message sent successfully
				default:
					// Client buffer full, drop message (non-blocking)
					log.Trace("Dashboard client buffer full, dropping message")
				}
			}
			b.mu.RUnlock()
		}
	}
}

// writeToClient writes messages from the client channel to the WebSocket connection
func (b *Broadcaster) writeToClient(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard client writer panic recovered: %v", r)
		}
		b.unregister <- conn
	}()

	b.mu.RLock()
	ch, ok := b.clients[conn]
	b.mu.RUnlock()

	if !ok {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-ch:
			if !ok {
				// Channel closed
				return
			}
			if err := conn.WriteJSON(message); err != nil {
				log.Debugf("Dashboard error writing to client: %v", err)
				return
			}
		}
	}
}

// Send broadcasts a message to all connected clients
func (b *Broadcaster) Send(msg Message) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard send panic recovered: %v", r)
		}
	}()

	select {
	case b.broadcast <- msg:
		// Message queued successfully
	default:
		// Broadcast channel full, drop message
		log.Trace("Dashboard broadcast channel full, dropping message")
	}
}

// RegisterClient registers a new WebSocket connection
func (b *Broadcaster) RegisterClient(conn *websocket.Conn) {
	b.register <- conn
}

// UnregisterClient unregisters a WebSocket connection
func (b *Broadcaster) UnregisterClient(conn *websocket.Conn) {
	b.unregister <- conn
}

// ClientCount returns the current number of connected clients
func (b *Broadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}
