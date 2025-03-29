package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"

	"github.com/spectre-xenon/websocket"
)

var dialer = &websocket.Dialer{
	CompressionConfig: websocket.CompressionConfig{
		Enabled: true,
		// force compression
		CompressionThreshold: 1,
	},
}

func main() {
	ws, _, err := dialer.Dial("ws://localhost:8080")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer ws.Close()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		select {
		default:
			_, err := ws.SendMessage(scanner.Bytes(), websocket.TextMessage)
			if err != nil {
				fmt.Println(err)
				return
			}

			_, payload, err := ws.NextMessage()
			if err != nil {
				fmt.Println(err)
				return
			}

			fmt.Println(string(payload))
		case <-sc:
			return
		}
	}

}
