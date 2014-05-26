// Package local_server implements a http Server for handling each peer communication
package local_server

import (
	"fmt"
	"net"
	"net/http"
)

type LocalServer struct {
	PeerId     string
	Port       int
	HttpServer *http.Server
}

type requestHandler struct{}

func (req requestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Received Request")
	fmt.Fprintf(w, "hi!!")
}

// New returns a http local server for peerId
func New(peerId string) *LocalServer {
	tryPorts := make([]int, 100)
	for i := 6881; i < 6981; i++ {
		tryPorts = append(tryPorts, i)
	}
	serverChan := make(chan *http.Server)
	go (func(serverChan chan *http.Server) {
		var server *http.Server
		for _, port := range tryPorts {
			server = &http.Server{
				Addr:    fmt.Sprintf(":%d", port),
				Handler: requestHandler{},
			}
			listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				continue
			} else {
				serverChan <- server
				server.Serve(listener)
			}
		}
		panic("No port available")
	})(serverChan)
	server := <-serverChan
	var port int
	fmt.Sscanf(server.Addr, ":%d", &port)
	return &LocalServer{
		PeerId:     peerId,
		Port:       port,
		HttpServer: server,
	}
}
