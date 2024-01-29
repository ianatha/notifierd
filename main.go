package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type Message struct {
	Subscribe   string `json:"subscribe"`
	Unsubscribe string `json:"unsubscribe"`
}

type Event struct {
	Event string   `json:"event"`
	Data  []string `json:"data"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	server := NewServer()

	r := mux.NewRouter()
	r.HandleFunc("/{chan}/", server.handlePostMessage).Methods("POST")
	r.HandleFunc("/ws", server.handleWebSocket)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
