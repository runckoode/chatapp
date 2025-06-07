package main

import (
	"sync"
)

type Room struct {
	ID      string
	Clients map[string]*Client
	Mutex   sync.Mutex
}
