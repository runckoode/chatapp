package main

import (
"encoding/json"
"io/ioutil"
"log"
"net/http"
"strings"
"sync"
"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// --- ESTRUCTURAS ---
type Client struct {
ID     string
Conn   *websocket.Conn
Role   string
RoomID string
Send   chan string
}

type Room struct {
ID      string
Clients map[string]*Client
Mutex   sync.Mutex
}

type Hub struct {
Rooms   map[string]*Room
Clients map[string]*Client
Mutex   sync.Mutex
}

type AIResponse struct {
Message string `json:"message"`
}

var upgrader = websocket.Upgrader{
CheckOrigin: func(r *http.Request) bool { return true },
}

// --- MÉTODOS DEL HUB ---
func newHub() *Hub {
return &Hub{
Rooms:   make(map[string]*Room),
Clients: make(map[string]*Client),
}
}

func (h *Hub) addClient(client *Client) {
h.Mutex.Lock()
h.Clients[client.ID] = client
if client.RoomID != "" {
if room, ok := h.Rooms[client.RoomID]; ok {
room.Mutex.Lock()
room.Clients[client.ID] = client
room.Mutex.Unlock()
}
}
h.Mutex.Unlock()
}

func (h *Hub) removeClient(client *Client) {
h.Mutex.Lock()
defer h.Mutex.Unlock()
delete(h.Clients, client.ID)
if client.RoomID != "" {
if room, ok := h.Rooms[client.RoomID]; ok {
room.Mutex.Lock()
delete(room.Clients, client.ID)
if len(room.Clients) == 0 {
delete(h.Rooms, room.ID)
}
room.Mutex.Unlock()
}
}
}

func (h *Hub) createRoom(client *Client) string {
roomID := uuid.New().String()
room := &Room{
ID:      roomID,
Clients: make(map[string]*Client),
}
room.Clients[client.ID] = client
client.RoomID = roomID
h.Mutex.Lock()
h.Rooms[roomID] = room
h.Mutex.Unlock()
return roomID
}

func (h *Hub) joinRoom(client *Client, roomID string) bool {
h.Mutex.Lock()
room, ok := h.Rooms[roomID]
h.Mutex.Unlock()
if !ok {
return false
}
room.Mutex.Lock()
defer room.Mutex.Unlock()
if _, exists := room.Clients[client.ID]; exists {
return false
}
room.Clients[client.ID] = client
client.RoomID = roomID
return true
}

func (h *Hub) listRooms() []string {
h.Mutex.Lock()
defer h.Mutex.Unlock()
ids := make([]string, 0, len(h.Rooms))
for id := range h.Rooms {
ids = append(ids, id)
}
return ids
}

// --- HANDLERS DE WEBSOCKET ---
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
msg := strings.TrimSpace(string(message))
switch client.Role {
case "admin":
handleAdminMessage(hub, client, msg)
case "client":
handleClientMessage(hub, client, msg)
}
}
}

func handleAdminMessage(hub *Hub, client *Client, msg string) {
switch {
case msg == "LIST_ROOMS":
client.Send <- "ROOMS:" + strings.Join(hub.listRooms(), ",")
case strings.HasPrefix(msg, "JOIN_ROOM:"):
roomID := strings.TrimPrefix(msg, "JOIN_ROOM:")
if hub.joinRoom(client, roomID) {
client.Send <- "JOINED_ROOM:" + roomID
} else {
client.Send <- "ERROR: No se pudo unir a la sala"
}
case msg == "LEAVE_CHAT" && client.RoomID != "":
hub.Mutex.Lock()
if room, ok := hub.Rooms[client.RoomID]; ok {
room.Mutex.Lock()
delete(room.Clients, client.ID)
room.Mutex.Unlock()
}
client.RoomID = ""
hub.Mutex.Unlock()
client.Send <- "Has salido de la sala."
case client.RoomID != "":
hub.Mutex.Lock()
if room, ok := hub.Rooms[client.RoomID]; ok {
room.Mutex.Lock()
for _, c := range room.Clients {
if c.ID != client.ID && c.Role == "client" {
c.Send <- msg
}
}
room.Mutex.Unlock()
}
hub.Mutex.Unlock()
default:
}
}

func handleClientMessage(hub *Hub, client *Client, msg string) {
hub.Mutex.Lock()
room, ok := hub.Rooms[client.RoomID]
if ok {
room.Mutex.Lock()
hasAdmin := false
for _, c := range room.Clients {
if c.Role == "admin" {
hasAdmin = true
c.Send <- "[CLIENT " + client.ID[:4] + "]: " + msg
}
}
if !hasAdmin {
client.Send <- "No hay agentes disponibles en este momento."
}
room.Mutex.Unlock()
}
hub.Mutex.Unlock()
}

func (client *Client) writePump() {
for msg := range client.Send {
err := client.Conn.WriteMessage(websocket.TextMessage, []byte(msg))
if err != nil {
break
}
}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
conn, err := upgrader.Upgrade(w, r, nil)
if err != nil {
return
}
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
		_, pwdMsg, err := conn.ReadMessage()
		if err != nil || string(pwdMsg) != "password" {
			conn.WriteMessage(websocket.TextMessage, []byte("ERROR: Invalid password"))
			conn.Close()
			return
		}
		roomID := hub.createRoom(client)
		client.Send <- "WELCOME! Your room ID is: " + roomID
	} else if role == "admin" {
		client.Send <- "Conectado como admin."
	} else {
		conn.WriteMessage(websocket.TextMessage, []byte("ERROR: Unknown role"))
		conn.Close()
		return
	}
	hub.addClient(client)
	go client.writePump()
	go client.readPump(hub)
}

// --- TRABAJADOR DE IA ---
func runAIAdminWorker() {
time.Sleep(2 * time.Second) // Esperar a que el servidor principal inicie

	wsURL := "ws://localhost:8080/ws"
	var currentRoomID string

	connectAndWork := func() {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			log.Printf("Trabajador IA: Error al conectar: %v. Reintentando...", err)
			return
		}
		defer conn.Close()
		log.Println("Trabajador IA: Conectado al servidor de chat.")

		if err := conn.WriteMessage(websocket.TextMessage, []byte("admin")); err != nil {
			log.Printf("Trabajador IA: Error al enviar rol: %v", err)
			return
		}
		_, _, _ = conn.ReadMessage() // Leer bienvenida

		// Bucle para leer mensajes del servidor
		go func() {
			for {
				_, msgBytes, err := conn.ReadMessage()
				if err != nil {
					return
				}
				msg := string(msgBytes)

				if strings.HasPrefix(msg, "[CLIENT") {
					log.Printf("Trabajador IA: Mensaje recibido, llamando a API externa...")
					resp, err := http.Get("http://localhost:8000/latest-admin-message")
					if err != nil {
						log.Printf("Trabajador IA: Error en llamada a API: %v", err)
						continue
					}

					body, _ := ioutil.ReadAll(resp.Body)
					resp.Body.Close()

					var aiResp AIResponse
					if json.Unmarshal(body, &aiResp) == nil {
						parts := strings.SplitN(aiResp.Message, "\n\n", 2)
						finalResponse := parts[0]
						conn.WriteMessage(websocket.TextMessage, []byte(finalResponse))
					}
				} else if strings.HasPrefix(msg, "ROOMS:") {
					roomsStr := strings.TrimPrefix(msg, "ROOMS:")
					if roomsStr != "" {
						rooms := strings.Split(roomsStr, ",")
						if len(rooms) > 0 && currentRoomID == "" {
							roomToJoin := rooms[0]
							log.Printf("Trabajador IA: Uniéndose a la sala %s", roomToJoin)
							conn.WriteMessage(websocket.TextMessage, []byte("JOIN_ROOM:"+roomToJoin))
							currentRoomID = roomToJoin
						}
					}
				}
			}
		}()

		// Bucle para buscar salas periódicamente
		for {
			time.Sleep(10 * time.Second)
			if currentRoomID == "" {
				if err := conn.WriteMessage(websocket.TextMessage, []byte("LIST_ROOMS")); err != nil {
					return
				}
			}
		}
	}

	for {
		connectAndWork()
		time.Sleep(5 * time.Second)
	}
}

// --- version  escribir directo ---
func main() {
hub := newHub()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	go runAIAdminWorker() // Lanza el trabajador de IA en una goroutine

	log.Println("Servidor de Chat iniciado en :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
