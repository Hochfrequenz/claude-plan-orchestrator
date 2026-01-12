package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSEEvent represents a server-sent event
type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// SSEHub manages SSE connections
type SSEHub struct {
	clients    map[chan SSEEvent]bool
	broadcast  chan SSEEvent
	register   chan chan SSEEvent
	unregister chan chan SSEEvent
	mu         sync.RWMutex
}

// NewSSEHub creates a new SSE hub
func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients:    make(map[chan SSEEvent]bool),
		broadcast:  make(chan SSEEvent),
		register:   make(chan chan SSEEvent),
		unregister: make(chan chan SSEEvent),
	}
}

// Run starts the SSE hub
func (h *SSEHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client <- event:
				default:
					close(client)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends an event to all clients
func (h *SSEHub) Broadcast(event SSEEvent) {
	h.broadcast <- event
}

func (s *Server) sseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Create client channel
		client := make(chan SSEEvent)
		s.sseHub.register <- client

		// Cleanup on disconnect
		notify := r.Context().Done()
		go func() {
			<-notify
			s.sseHub.unregister <- client
		}()

		// Stream events
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		for event := range client {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\n", event.Type)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
