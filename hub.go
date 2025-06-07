package main

import (
	_ "strings"
	"sync"
)

type Hub struct {
	Clients map[string]*Client
	Rooms   map[string]*Room
	Admin   *Client
	Mutex   sync.Mutex
}

func newHub() *Hub {
	return &Hub{
		Clients: make(map[string]*Client),
		Rooms:   make(map[string]*Room),
	}
}

func (hub *Hub) addClient(client *Client) {
	hub.Mutex.Lock()
	defer hub.Mutex.Unlock()
	hub.Clients[client.ID] = client
	if client.Role == "admin" {
		hub.Admin = client
	}
}

func (hub *Hub) removeClient(client *Client) {
	hub.Mutex.Lock()
	defer hub.Mutex.Unlock()
	delete(hub.Clients, client.ID)
	if client.Role == "admin" {
		hub.Admin = nil
	}
	if client.Role == "client" && client.RoomID != "" {
		if room, ok := hub.Rooms[client.RoomID]; ok {
			room.Mutex.Lock()
			delete(room.Clients, client.ID)
			room.Mutex.Unlock()
			if len(room.Clients) == 0 {
				delete(hub.Rooms, client.RoomID)
			}
		}
	}
}

func (hub *Hub) createRoom(client *Client) string {
	roomID := client.ID // Each client gets a unique room by their ID
	room := &Room{
		ID:      roomID,
		Clients: make(map[string]*Client),
	}
	room.Clients[client.ID] = client
	client.RoomID = roomID
	hub.Rooms[roomID] = room
	return roomID
}

func (hub *Hub) joinRoom(client *Client, roomID string) bool {
	hub.Mutex.Lock()
	defer hub.Mutex.Unlock()
	room, exists := hub.Rooms[roomID]
	if !exists {
		return false
	}
	room.Mutex.Lock()
	defer room.Mutex.Unlock()
	for _, c := range room.Clients {
		if c.Role == "admin" {
			return false // Only one admin per room
		}
	}
	room.Clients[client.ID] = client
	client.RoomID = roomID
	return true
}

func (hub *Hub) listRooms() []string {
	hub.Mutex.Lock()
	defer hub.Mutex.Unlock()
	var rooms []string
	for id := range hub.Rooms {
		rooms = append(rooms, id)
	}
	return rooms
}
