package webrtc

import (
	"github.com/pion/webrtc/v3"
)

type Peer struct {
	Id         string
	Connection *webrtc.PeerConnection
}

type PeerEvent struct {
	Reason string
	PeerId string
}

type PeerManager struct {
	Peers         map[string]*Peer
	PeerEventChan chan PeerEvent
}

func NewPeerManager() *PeerManager {
	return &PeerManager{}
}

// Public

func (manager *PeerManager) Start() {
	manager.PeerEventChan = make(chan PeerEvent)
	manager.Peers = make(map[string]*Peer)

	go manager.listenPeerEvents()
}

func (manager *PeerManager) HandleSessionDescriptionOffer(id string, sessionDescriptionOffer string) string {
	manager.Peers[id] = &Peer{
		Id:         id,
		Connection: CreatePeerConnection(sessionDescriptionOffer),
	}

	// Output the answer in base64 so we can paste it in browser
	return Encode(manager.Peers[id].Connection.LocalDescription())
}

// Private

func (manager *PeerManager) listenPeerEvents() {
	for {
		peerEvent := <-manager.PeerEventChan
		manager.handlePeerEvent(peerEvent)
	}
}

func (manager *PeerManager) handlePeerEvent(peerEvent PeerEvent) {

}
