package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type Server struct {
	channels map[string]*Channel
	lock     sync.RWMutex
}

type Channel struct {
	clients map[*Client]bool
	lock    sync.RWMutex
}

type Client struct {
	conn     *websocket.Conn
	userid   string
	channels map[string]bool
	lock     sync.Mutex
}

func NewServer() *Server {
	return &Server{channels: make(map[string]*Channel)}
}

func (s *Server) subscribeClientToChannel(client *Client, channelName string) {
	s.lock.Lock()
	if _, ok := s.channels[channelName]; !ok {
		s.channels[channelName] = &Channel{clients: make(map[*Client]bool)}
	}
	channel := s.channels[channelName]
	s.lock.Unlock()

	channel.lock.Lock()
	channel.clients[client] = true
	channel.lock.Unlock()

	client.lock.Lock()
	client.channels[channelName] = true
	client.lock.Unlock()

	log.Printf("Client subscribed to channel: %s", channelName)
	s.broadcastUpdatedMembers(channelName)
}

func (s *Server) unsubscribeClientFromChannel(client *Client, channelName string) {
	s.lock.Lock()
	if channel, ok := s.channels[channelName]; ok {
		channel.lock.Lock()
		delete(channel.clients, client)
		channel.lock.Unlock()
	}
	s.lock.Unlock()

	client.lock.Lock()
	delete(client.channels, channelName)
	client.lock.Unlock()

	log.Printf("Client unsubscribed from channel: %s", channelName)
	s.broadcastUpdatedMembers(channelName)
}

func (s *Server) broadcastUpdatedMembers(channelName string) {
	s.lock.RLock()
	channel, ok := s.channels[channelName]
	if !ok {
		s.lock.RUnlock()
		log.Printf("Channel %s not found", channelName)
		return
	}

	// Collecting userIDs of all clients in the channel
	var userIDs []string
	channel.lock.RLock()
	for client := range channel.clients {
		userIDs = append(userIDs, client.userid)
	}
	channel.lock.RUnlock()
	s.lock.RUnlock()

	// Prepare the message
	message, err := json.Marshal(Event{
		Event: "members",
		Chan:  channelName,
		Data:  userIDs,
	})
	if err != nil {
		log.Printf("Error marshalling userIDs: %v", err)
		return
	}

	// Broadcasting the message to all clients in the channel
	channel.lock.RLock()
	for client := range channel.clients {
		if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("Error broadcasting updated members to client: %v", err)
			client.conn.Close()
			delete(channel.clients, client)
		}
	}
	channel.lock.RUnlock()
}

func (s *Server) broadcastMessage(channel *Channel, message []byte) {
	channel.lock.RLock()
	defer channel.lock.RUnlock()

	for client := range channel.clients {
		if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("Error broadcasting message: %v", err)
			client.conn.Close()
			delete(channel.clients, client)
		}
	}
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chName := vars["chan"]

	s.lock.RLock()
	channel, ok := s.channels[chName]
	s.lock.RUnlock()

	if !ok {
		http.Error(w, "Channel not found", http.StatusNotFound)
		return
	}

	// Read message body
	message, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read message", http.StatusInternalServerError)
		return
	}
	r.Body.Close()

	// Broadcast message
	s.broadcastMessage(channel, message)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	uid := r.URL.Query().Get("uid")

	client := &Client{
		conn:     conn,
		userid:   uid,
		channels: make(map[string]bool),
	}

	defer func() {
		client.lock.Lock()
		for channelName := range client.channels {
			s.lock.Lock()
			if channel, ok := s.channels[channelName]; ok {
				channel.lock.Lock()
				delete(channel.clients, client)
				channel.lock.Unlock()
			}
			s.lock.Unlock()
		}
		client.lock.Unlock()
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Error unmarshalling message: %v", err)
			continue
		}

		if msg.Subscribe != "" {
			s.subscribeClientToChannel(client, msg.Subscribe)
		}
		if msg.Unsubscribe != "" {
			s.unsubscribeClientFromChannel(client, msg.Unsubscribe)
		}
	}
}
