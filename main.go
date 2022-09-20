//go:build !js
// +build !js

package main

import (
	"fmt"

	"github.com/webrtc-go/peer"
)

func main() {
	peerManager := peer.NewPeerManager()
	peerManager.Start()

	sdp := ""

	localSDP := peerManager.HandleSessionDescriptionOffer("1", sdp)

	fmt.Println(localSDP)
	// Block forever
	select {}
}
