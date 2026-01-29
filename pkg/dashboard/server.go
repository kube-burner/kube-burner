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
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now
	},
}

// Server manages the HTTP/WebSocket dashboard server
type Server struct {
	address     string
	port        int
	httpServer  *http.Server
	broadcaster *Broadcaster
	collector   *Collector
}

// NewServer creates a new dashboard server
func NewServer(broadcaster *Broadcaster, collector *Collector, address string, port int) *Server {
	return &Server{
		address:     address,
		port:        port,
		broadcaster: broadcaster,
		collector:   collector,
	}
}

// Start starts the HTTP server and broadcaster
func (s *Server) Start(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard server panic recovered: %v", r)
		}
	}()

	// Start broadcaster
	go s.broadcaster.Run(ctx)

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/snapshot", s.handleSnapshot)

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.address, s.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Infof("Starting dashboard server on %s:%d", s.address, s.port)

	// Start HTTP server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("dashboard server error: %w", err)
		}
	}()

	// Wait for either error or context cancellation
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return nil
	case <-time.After(100 * time.Millisecond):
		// Server started successfully
		log.Infof("Dashboard server started successfully, connect at ws://%s:%d/ws", s.address, s.port)
		return nil
	}
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard shutdown panic recovered: %v", r)
		}
	}()

	if s.httpServer == nil {
		return nil
	}

	log.Info("Shutting down dashboard server")
	return s.httpServer.Shutdown(ctx)
}

// handleWebSocket handles WebSocket upgrade and client communication
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard WebSocket handler panic recovered: %v", r)
		}
	}()

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("Dashboard WebSocket upgrade failed: %v", err)
		return
	}

	// Register client with broadcaster
	s.broadcaster.RegisterClient(conn)

	// Send initial snapshot
	if s.collector != nil {
		snapshot := s.collector.GetSnapshot()
		if err := conn.WriteJSON(Message{
			Type:      MessageTypeSnapshot,
			Timestamp: time.Now(),
			Data:      snapshot,
		}); err != nil {
			log.Debugf("Dashboard error sending initial snapshot: %v", err)
			s.broadcaster.UnregisterClient(conn)
			return
		}
	}

	// Read messages from client (currently just for keep-alive/ping)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Debugf("Dashboard WebSocket error: %v", err)
			}
			s.broadcaster.UnregisterClient(conn)
			break
		}
	}
}

// handleHealth returns the health status of the dashboard server
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard health handler panic recovered: %v", r)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	health := map[string]interface{}{
		"status":        "ok",
		"timestamp":     time.Now(),
		"clientCount":   s.broadcaster.ClientCount(),
		"broadcasterOk": s.broadcaster != nil,
		"collectorOk":   s.collector != nil,
	}

	json.NewEncoder(w).Encode(health)
}

// handleSnapshot returns the current dashboard snapshot as JSON
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Dashboard snapshot handler panic recovered: %v", r)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}()

	if s.collector == nil {
		http.Error(w, "Collector not available", http.StatusServiceUnavailable)
		return
	}

	snapshot := s.collector.GetSnapshot()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		log.Errorf("Dashboard error encoding snapshot: %v", err)
	}
}
