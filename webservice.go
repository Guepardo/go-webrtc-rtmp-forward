package main

import (
	"log"
	"net/http"

	"github.com/go-webrtc-rtmp-forward/controllers"
	"github.com/go-webrtc-rtmp-forward/webrtc"
	"github.com/gorilla/mux"
)

func NewServer() Server {
	server := Server{}
	server.Initialize()

	return server
}

type Server struct {
	Router      *mux.Router
	PeerManager *webrtc.PeerManager
}

func (server *Server) Initialize() {
	server.initializeServices()
	server.initializeRouters()
}

func (server *Server) initializeServices() {
	server.PeerManager = webrtc.NewPeerManager()
	server.PeerManager.Start()
}

func (server *Server) initializeRouters() {
	peerController := controllers.PeerController{
		PeerManager: server.PeerManager,
	}

	server.Router = mux.NewRouter()
	server.Router.HandleFunc("/api/peer", peerController.Create).Methods("POST")
}

func (server *Server) Start() {
	log.Fatal(http.ListenAndServe(":5000", server.Router))
}
