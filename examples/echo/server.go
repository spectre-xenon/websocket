package main

import (
	"fmt"
	"net/http"

	"github.com/spectre-xenon/websocket"
)

var upgrader = &websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Accept connection from any origin
		return true
	},
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		err := upgrader.Upgrade(w, r)
		if err != nil {
			fmt.Println(err)
		}
	})

	http.ListenAndServe(":8080", http.DefaultServeMux)
}

