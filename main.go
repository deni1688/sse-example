package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type connection struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	ctx     context.Context
}

type Broker struct {
	conns      map[string]*connection
	connsMutex sync.RWMutex
}

func NewBroker() *Broker {
	return &Broker{conns: map[string]*connection{}}
}

func (b *Broker) NotifyAll(event Event) {
	b.connsMutex.RLock()
	defer b.connsMutex.RUnlock()

	for client, connection := range b.conns {
		fmt.Println("Sending event to connection", client)
		eventBytes, err := json.Marshal(event)
		fmt.Fprintf(connection.writer, "data: %s\n\n", eventBytes)
		if err != nil {
			b.RemoveConnection(client)
			continue
		}

		connection.flusher.Flush()
	}
}

func (b *Broker) AddConnection(id string, connection *connection) {
	b.connsMutex.Lock()
	b.conns[id] = connection
	b.connsMutex.Unlock()
}

func (b *Broker) RemoveConnection(client string) {
	b.connsMutex.Lock()
	defer b.connsMutex.Unlock()
	fmt.Println("Removing connection", client)
	delete(b.conns, client)
}

type Event struct {
	VehicleID string `json:"vehicleId"`
	Available bool   `json:"available"`
}

func endlessProcess(broker *Broker) {
	for {
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		available := time.Now().UnixNano()%2 == 0
		broker.NotifyAll(Event{
			VehicleID: id,
			Available: available,
		})
		time.Sleep(3 * time.Second)
	}
}

func getEventHandler(broker *Broker) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		fmt.Println("New connection", req.RemoteAddr)

		flusher, ok := rw.(http.Flusher)

		if !ok {
			http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
			return
		}

		ctx := req.Context()

		broker.AddConnection(req.RemoteAddr, &connection{
			writer:  rw,
			flusher: flusher,
			ctx:     ctx,
		})

		defer func() {
			broker.RemoveConnection(req.RemoteAddr)
		}()

		rw.Header().Set("Content-Type", "text/event-stream")
		rw.Header().Set("Cache-Control", "no-cache")
		rw.Header().Set("Connection", "keep-alive")
		rw.Header().Set("Access-Control-Allow-Origin", "*")

		<-ctx.Done()
	}
}

func main() {
	done := make(chan struct{})
	broker := NewBroker()

	r := http.NewServeMux()
	r.HandleFunc("/vehicle-update-events", getEventHandler(broker))
	r.Handle("/", http.FileServer(http.Dir("./public")))

	go endlessProcess(broker)

	go func() {
		fmt.Println("Starting server")
		log.Fatalln("Server error", http.ListenAndServe(":9000", r))
	}()

	<-done
}
