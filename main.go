package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net/http"
	"sync"
)

type Server struct {
	channels map[string]*Channel
	lock     sync.RWMutex
}

type Channel struct {
	clients map[*websocket.Conn]string
	lock    sync.RWMutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewServer() *Server {
	return &Server{channels: make(map[string]*Channel)}
}

func (s *Server) broadcastMessage(channel *Channel, message []byte) {
	channel.lock.RLock()
	defer channel.lock.RUnlock()

	for client := range channel.clients {
		if err := client.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("Error broadcasting message: %v", err)
			client.Close()
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
	vars := mux.Vars(r)
	chName := vars["chan"]
	userID := r.URL.Query().Get("uid")

	if userID == "" {
		http.Error(w, "?uid is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	s.lock.Lock()
	if _, ok := s.channels[chName]; !ok {
		s.channels[chName] = &Channel{clients: make(map[*websocket.Conn]string)}
	}
	channel := s.channels[chName]

	// Send list of connected userIDs to the new client
	var userIDs []string
	for _, uid := range channel.clients {
		userIDs = append(userIDs, uid)
	}
	usersList, _ := json.Marshal(userIDs)
	conn.WriteMessage(websocket.TextMessage, usersList)

	channel.lock.Lock()
	channel.clients[conn] = userID
	channel.lock.Unlock()
	s.lock.Unlock()

	connectMsg := fmt.Sprintf("User %s connected to channel: %s", userID, chName)
	log.Println(connectMsg)
	s.broadcastMessage(channel, []byte(connectMsg))

	// Handle disconnect
	defer func() {
		channel.lock.Lock()
		delete(channel.clients, conn)
		channel.lock.Unlock()
		conn.Close()

		disconnectMsg := fmt.Sprintf("User %s disconnected from channel: %s", userID, chName)
		log.Println(disconnectMsg)
		s.broadcastMessage(channel, []byte(disconnectMsg))
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}
		// Handle incoming messages or pings here if needed
	}
}

func main() {
	server := NewServer()

	r := mux.NewRouter()
	r.HandleFunc("/{chan}/msg", server.handlePostMessage).Methods("POST")
	r.HandleFunc("/{chan}/", server.handleWebSocket)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
