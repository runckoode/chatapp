package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (client *Client) readPump(hub *Hub) {
	defer func() {
		hub.removeClient(client)
		client.Conn.Close()
	}()
	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			break
		}
		msg := string(message)
		if client.Role == "admin" {
			if msg == "LIST_ROOMS" {
				rooms := hub.listRooms()
				client.Send <- "ROOMS:" + strings.Join(rooms, ",")
				continue
			}
			if strings.HasPrefix(msg, "JOIN_ROOM:") {
				roomID := strings.TrimPrefix(msg, "JOIN_ROOM:")
				if hub.joinRoom(client, roomID) {
					client.Send <- "JOINED_ROOM:" + roomID
				} else {
					client.Send <- "ERROR: Cannot join room or already joined"
				}
				continue
			}
			// Admin message to room
			if client.RoomID != "" {
				hub.Mutex.Lock()
				room, ok := hub.Rooms[client.RoomID]
				hub.Mutex.Unlock()
				if ok {
					room.Mutex.Lock()
					for _, c := range room.Clients {
						if c.ID != client.ID {
							c.Send <- "[ADMIN]: " + msg
						}
					}
					room.Mutex.Unlock()
				}
			}
		} else if client.Role == "client" {
			// Client message to room
			if client.RoomID != "" {
				hub.Mutex.Lock()
				room, ok := hub.Rooms[client.RoomID]
				hub.Mutex.Unlock()
				if ok {
					room.Mutex.Lock()
					for _, c := range room.Clients {
						if c.ID != client.ID {
							c.Send <- "[CLIENT]: " + msg
						}
					}
					room.Mutex.Unlock()
				}
			}
		}
	}
}

func (client *Client) writePump() {
	for msg := range client.Send {
		client.Conn.WriteMessage(websocket.TextMessage, []byte(msg))
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	// Read role
	_, roleMsg, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return
	}
	role := string(roleMsg)
	client := &Client{
		ID:   uuid.New().String(),
		Conn: conn,
		Role: role,
		Send: make(chan string, 256),
	}

	if role == "client" {
		// Read password
		_, pwdMsg, err := conn.ReadMessage()
		if err != nil || string(pwdMsg) != "password" {
			conn.WriteMessage(websocket.TextMessage, []byte("ERROR: Invalid password"))
			conn.Close()
			return
		}
		roomID := hub.createRoom(client)
		client.Send <- "WELCOME! Your room ID is: " + roomID
	} else if role == "admin" {
		client.Send <- "WELCOME ADMIN"
	} else {
		conn.WriteMessage(websocket.TextMessage, []byte("ERROR: Unknown role"))
		conn.Close()
		return
	}

	hub.addClient(client)
	go client.writePump()
	go client.readPump(hub)
}

func main() {
	hub := newHub()
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})
	go func() {
		log.Println("Server started on :8080")
		log.Fatal(http.ListenAndServe(":8080", nil))
	}()

	// Optional: Admin CLI for demo/testing
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Type 'rooms' to list active rooms: ")
		cmd, _ := reader.ReadString('\n')
		cmd = strings.TrimSpace(cmd)
		if cmd == "rooms" {
			rooms := hub.listRooms()
			fmt.Println("Active rooms:")
			for _, id := range rooms {
				fmt.Println(" -", id)
			}
		}
	}
}
