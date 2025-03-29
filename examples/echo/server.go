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
	CompressionConfig: websocket.CompressionConfig{
		Enabled:           true,
		IsContextTakeover: true,
		// force compression
		CompressionThreshold: 1,
	},
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer ws.Close()

		for {
			mt, payload, err := ws.NextMessage()
			if err != nil {
				fmt.Println(err)
				return
			}

			_, err = ws.SendMessage(payload, mt)
			if err != nil {
				fmt.Println(err)
				return
			}

		}
	})

	http.ListenAndServe(":8080", http.DefaultServeMux)
}
