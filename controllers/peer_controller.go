package controllers

import (
	"log"
	"net/http"

	"github.com/go-webrtc-rtmp-forward/webrtc"
	"github.com/gorilla/mux"
)

type Server struct {
	Router *mux.Router
}

type PeerController struct {
	PeerManager *webrtc.PeerManager
}

func (peerController *PeerController) Create(w http.ResponseWriter, r *http.Request) {
	log.Println("lol")
}
