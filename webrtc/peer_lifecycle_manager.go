package webrtc

import (
	"fmt"
	"io"
	"time"

	"github.com/go-webrtc-rtmp-forward/transcode"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

type PeerLifeCycleManager struct {
	PeerId         string
	PeerEventChan  chan PeerEvent
	PeerConnection *webrtc.PeerConnection
	Transcode      *transcode.Transcode
}

func (manager *PeerLifeCycleManager) OnTrack(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
	go func() {
		ticker := time.NewTicker(time.Second * 3)
		for range ticker.C {
			if rtcpErr := manager.PeerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}}); rtcpErr != nil {
				fmt.Println(rtcpErr)
			}
		}
	}()

	for {
		rtpPacket, _, readErr := track.ReadRTP()

		if readErr != nil {
			if readErr == io.EOF {
				return
			}
			panic(readErr)
		}

		codecType := track.Kind()

		manager.Transcode.HandleRTPPacket(rtpPacket, codecType)
	}
}

func (manager *PeerLifeCycleManager) OnICEConnectionStateChange(connectionState webrtc.ICEConnectionState) {
	fmt.Printf("Connection State has changed %s \n", connectionState.String())

	if connectionState == webrtc.ICEConnectionStateConnected {
		fmt.Println("Ctrl+C the remote client to stop the demo")
	}
}

func (manager *PeerLifeCycleManager) OnConnectionStateChange(s webrtc.PeerConnectionState) {
	fmt.Printf("Peer Connection State has changed: %s\n", s.String())

	if s == webrtc.PeerConnectionStateFailed {
		// Wait until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
		// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
		// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.
		fmt.Println("Done forwarding")
		// os.Exit(0)
	}
}
