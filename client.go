package main

import "github.com/gorilla/websocket"

type Client struct {
	ID     string
	Conn   *websocket.Conn
	Role   string // "admin" or "client"
	RoomID string
	Send   chan string
}
