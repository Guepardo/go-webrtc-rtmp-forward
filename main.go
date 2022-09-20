//go:build !js
// +build !js

package main

import (
	"fmt"

	"github.com/go-webrtc-rtmp-forward/webrtc"
)

func main() {
	peerManager := webrtc.NewPeerManager()
	peerManager.Start()

	sdp := ""

	localSDP := peerManager.HandleSessionDescriptionOffer("1", sdp)

	fmt.Println(localSDP)
	// Block forever
	select {}
}
