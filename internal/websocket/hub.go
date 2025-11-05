package websocket

import (
	"log"
	"sync"
)

type Hub struct {
	clients        map[int64]map[*Client]bool
	mu             sync.RWMutex
	Register       chan *Client
	Unregister     chan *Client
	DisconnectUser chan int64
}

func NewHub() *Hub {
	return &Hub{
		clients:        make(map[int64]map[*Client]bool),
		Register:       make(chan *Client),
		Unregister:     make(chan *Client),
		DisconnectUser: make(chan int64),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.registerClient(client)
		case client := <-h.Unregister:
			h.unregisterClient(client)
		case userID := <-h.DisconnectUser:
			h.disconnectUser(userID)
		}
	}
}

func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[client.UserID]; !ok {
		h.clients[client.UserID] = make(map[*Client]bool)
	}
	h.clients[client.UserID][client] = true
	log.Printf("Hub: Client for user %d registered", client.UserID)
}

func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if userClients, ok := h.clients[client.UserID]; ok {
		if _, ok := userClients[client]; ok {
			delete(userClients, client)
			close(client.send)
			if len(userClients) == 0 {
				delete(h.clients, client.UserID)
			}
			log.Printf("Hub: Client for user %d unregistered", client.UserID)
		}
	}
}

func (h *Hub) disconnectUser(userID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if userClients, ok := h.clients[userID]; ok {
		log.Printf("Hub: Disconnecting all %d clients for user %d", len(userClients), userID)
		for client := range userClients {
			close(client.send)
		}
		delete(h.clients, userID)
	}
}

func (h *Hub) PublishEvent(userID int64, eventData []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if userClients, ok := h.clients[userID]; ok {
		for client := range userClients {
			select {
			case client.send <- eventData:
			default:
				log.Printf("WARN: Client for user %d send buffer is full. Dropping message.", userID)
			}
		}
	}
}
