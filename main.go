//go:build !js
// +build !js

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/webrtc-go/signal"
)

type udpConn struct {
	conn        *net.UDPConn
	port        int
	payloadType uint8
}

func main() {
	// Everything below is the Pion WebRTC API! Thanks for using it ❤️.

	// Create a MediaEngine object to configure the supported codec
	m := &webrtc.MediaEngine{}

	// Setup the codecs you want to use.
	// We'll use a VP8 and Opus but you can also define your own
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil},
	}, webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil},
	}, webrtc.RTPCodecTypeAudio); err != nil {
		panic(err)
	}

	// Create a InterceptorRegistry. This is the user configurable RTP/RTCP Pipeline.
	// This provides NACKs, RTCP Reports and other features. If you use `webrtc.NewPeerConnection`
	// this is enabled by default. If you are manually managing You MUST create a InterceptorRegistry
	// for each PeerConnection.
	i := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		panic(err)
	}

	// Create the API object with the MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i))

	// Prepare the configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Create a new RTCPeerConnection
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}
	defer func() {
		if cErr := peerConnection.Close(); cErr != nil {
			fmt.Printf("cannot close peerConnection: %v\n", cErr)
		}
	}()

	// Allow us to receive 1 audio track, and 1 video track
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		panic(err)
	} else if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}

	// Create a local addr
	var laddr *net.UDPAddr
	if laddr, err = net.ResolveUDPAddr("udp", "127.0.0.1:"); err != nil {
		panic(err)
	}

	// Prepare udp conns
	// Also update incoming packets with expected PayloadType, the browser may use
	// a different value. We have to modify so our stream matches what rtp-forwarder.sdp expects
	udpConns := map[string]*udpConn{
		"audio": {port: 4000, payloadType: 111},
		"video": {port: 4002, payloadType: 96},
	}

	for _, c := range udpConns {
		// Create remote addr
		var raddr *net.UDPAddr
		if raddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", c.port)); err != nil {
			panic(err)
		}

		// Dial udp
		if c.conn, err = net.DialUDP("udp", laddr, raddr); err != nil {
			panic(err)
		}

		defer func(conn net.PacketConn) {
			if closeErr := conn.Close(); closeErr != nil {
				panic(closeErr)
			}
		}(c.conn)
	}

	// Set a handler for when a new remote track starts, this handler will forward data to
	// our UDP listeners.
	// In your application this is where you would handle/process audio/video
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		// Retrieve udp connection
		c, ok := udpConns[track.Kind().String()]
		if !ok {
			return
		}

		// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
		go func() {
			ticker := time.NewTicker(time.Second * 2)
			for range ticker.C {
				if rtcpErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}}); rtcpErr != nil {
					fmt.Println(rtcpErr)
				}
			}
		}()

		b := make([]byte, 1500)
		rtpPacket := &rtp.Packet{}
		for {
			// Read
			n, _, readErr := track.Read(b)
			if readErr != nil {
				panic(readErr)
			}

			// Unmarshal the packet and update the PayloadType
			if err = rtpPacket.Unmarshal(b[:n]); err != nil {
				panic(err)
			}
			rtpPacket.PayloadType = c.payloadType

			// Marshal into original buffer with updated PayloadType
			if n, err = rtpPacket.MarshalTo(b); err != nil {
				panic(err)
			}

			// Write
			if _, writeErr := c.conn.Write(b[:n]); writeErr != nil {
				// For this particular example, third party applications usually timeout after a short
				// amount of time during which the user doesn't have enough time to provide the answer
				// to the browser.
				// That's why, for this particular example, the user first needs to provide the answer
				// to the browser then open the third party application. Therefore we must not kill
				// the forward on "connection refused" errors
				var opError *net.OpError
				if errors.As(writeErr, &opError) && opError.Err.Error() == "write: connection refused" {
					continue
				}
				panic(err)
			}
		}
	})

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("Connection State has changed %s \n", connectionState.String())

		if connectionState == webrtc.ICEConnectionStateConnected {
			fmt.Println("Ctrl+C the remote client to stop the demo")
		}
	})

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", s.String())

		if s == webrtc.PeerConnectionStateFailed {
			// Wait until PeerConnection has had no network activity for 30 seconds or another failure. It may be reconnected using an ICE Restart.
			// Use webrtc.PeerConnectionStateDisconnected if you are interested in detecting faster timeout.
			// Note that the PeerConnection may come back from PeerConnectionStateDisconnected.
			fmt.Println("Done forwarding")
			os.Exit(0)
		}
	})

	// Wait for the offer to be pasted
	offer := webrtc.SessionDescription{}
	signal.Decode("eyJ0eXBlIjoib2ZmZXIiLCJzZHAiOiJ2PTBcclxubz0tIDcwMDU0MzM2MzQ0MzczOTI1MDIgMiBJTiBJUDQgMTI3LjAuMC4xXHJcbnM9LVxyXG50PTAgMFxyXG5hPWdyb3VwOkJVTkRMRSAwIDFcclxuYT1leHRtYXAtYWxsb3ctbWl4ZWRcclxuYT1tc2lkLXNlbWFudGljOiBXTVMgSEpYNnJQdkxEWEl1SnZrTk5wNEFDZDNaUncyNkt2OGZjbHlCXHJcbm09YXVkaW8gMzUyNzYgVURQL1RMUy9SVFAvU0FWUEYgMTExIDYzIDEwMyAxMDQgOSAwIDggMTA2IDEwNSAxMyAxMTAgMTEyIDExMyAxMjZcclxuYz1JTiBJUDQgMTg5LjEyMy42My41NlxyXG5hPXJ0Y3A6OSBJTiBJUDQgMC4wLjAuMFxyXG5hPWNhbmRpZGF0ZTo0MTgyMjkwMTE3IDEgdWRwIDIxMjIyNjI3ODMgMjgwNDoxNGM6M2Y4Nzo4MjgyOmM4MTY6OTA0OTo0NGQ6MjdhIDU2MjMyIHR5cCBob3N0IGdlbmVyYXRpb24gMCBuZXR3b3JrLWlkIDIgbmV0d29yay1jb3N0IDEwXHJcbmE9Y2FuZGlkYXRlOjE0MzQzMDE3ODggMSB1ZHAgMjEyMjE5NDY4NyAxOTIuMTY4LjAuMTAgMzUyNzYgdHlwIGhvc3QgZ2VuZXJhdGlvbiAwIG5ldHdvcmstaWQgMSBuZXR3b3JrLWNvc3QgMTBcclxuYT1jYW5kaWRhdGU6Mjc0MTg4MTk5MiAxIHVkcCAxNjg1OTg3MDcxIDE4OS4xMjMuNjMuNTYgMzUyNzYgdHlwIHNyZmx4IHJhZGRyIDE5Mi4xNjguMC4xMCBycG9ydCAzNTI3NiBnZW5lcmF0aW9uIDAgbmV0d29yay1pZCAxIG5ldHdvcmstY29zdCAxMFxyXG5hPWNhbmRpZGF0ZTozMDgzNTU1MzgxIDEgdGNwIDE1MTgyODMwMDcgMjgwNDoxNGM6M2Y4Nzo4MjgyOmM4MTY6OTA0OTo0NGQ6MjdhIDkgdHlwIGhvc3QgdGNwdHlwZSBhY3RpdmUgZ2VuZXJhdGlvbiAwIG5ldHdvcmstaWQgMiBuZXR3b3JrLWNvc3QgMTBcclxuYT1jYW5kaWRhdGU6NDY5NjQ5ODM2IDEgdGNwIDE1MTgyMTQ5MTEgMTkyLjE2OC4wLjEwIDkgdHlwIGhvc3QgdGNwdHlwZSBhY3RpdmUgZ2VuZXJhdGlvbiAwIG5ldHdvcmstaWQgMSBuZXR3b3JrLWNvc3QgMTBcclxuYT1pY2UtdWZyYWc6dDljRVxyXG5hPWljZS1wd2Q6R216TGFTZkhQcEw1RHZBNjdtd3hxK0diXHJcbmE9aWNlLW9wdGlvbnM6dHJpY2tsZVxyXG5hPWZpbmdlcnByaW50OnNoYS0yNTYgQjY6Rjg6ODA6M0E6QTE6MkI6RTc6MjE6MzU6RTc6ODc6MUI6QTA6NTY6MDc6NkQ6MkE6Qjg6MjU6RDQ6NkI6Q0I6RTE6RkE6RTk6RUI6RTY6RkE6NUI6QjY6NTA6RTRcclxuYT1zZXR1cDphY3RwYXNzXHJcbmE9bWlkOjBcclxuYT1leHRtYXA6MSB1cm46aWV0ZjpwYXJhbXM6cnRwLWhkcmV4dDpzc3JjLWF1ZGlvLWxldmVsXHJcbmE9ZXh0bWFwOjIgaHR0cDovL3d3dy53ZWJydGMub3JnL2V4cGVyaW1lbnRzL3J0cC1oZHJleHQvYWJzLXNlbmQtdGltZVxyXG5hPWV4dG1hcDozIGh0dHA6Ly93d3cuaWV0Zi5vcmcvaWQvZHJhZnQtaG9sbWVyLXJtY2F0LXRyYW5zcG9ydC13aWRlLWNjLWV4dGVuc2lvbnMtMDFcclxuYT1leHRtYXA6NCB1cm46aWV0ZjpwYXJhbXM6cnRwLWhkcmV4dDpzZGVzOm1pZFxyXG5hPXNlbmRyZWN2XHJcbmE9bXNpZDpISlg2clB2TERYSXVKdmtOTnA0QUNkM1pSdzI2S3Y4ZmNseUIgMWFiMjk0Y2QtNWUxOS00ZmIxLWFhNzktOWJlYTc1OTAyMzMxXHJcbmE9cnRjcC1tdXhcclxuYT1ydHBtYXA6MTExIG9wdXMvNDgwMDAvMlxyXG5hPXJ0Y3AtZmI6MTExIHRyYW5zcG9ydC1jY1xyXG5hPWZtdHA6MTExIG1pbnB0aW1lPTEwO3VzZWluYmFuZGZlYz0xXHJcbmE9cnRwbWFwOjYzIHJlZC80ODAwMC8yXHJcbmE9Zm10cDo2MyAxMTEvMTExXHJcbmE9cnRwbWFwOjEwMyBJU0FDLzE2MDAwXHJcbmE9cnRwbWFwOjEwNCBJU0FDLzMyMDAwXHJcbmE9cnRwbWFwOjkgRzcyMi84MDAwXHJcbmE9cnRwbWFwOjAgUENNVS84MDAwXHJcbmE9cnRwbWFwOjggUENNQS84MDAwXHJcbmE9cnRwbWFwOjEwNiBDTi8zMjAwMFxyXG5hPXJ0cG1hcDoxMDUgQ04vMTYwMDBcclxuYT1ydHBtYXA6MTMgQ04vODAwMFxyXG5hPXJ0cG1hcDoxMTAgdGVsZXBob25lLWV2ZW50LzQ4MDAwXHJcbmE9cnRwbWFwOjExMiB0ZWxlcGhvbmUtZXZlbnQvMzIwMDBcclxuYT1ydHBtYXA6MTEzIHRlbGVwaG9uZS1ldmVudC8xNjAwMFxyXG5hPXJ0cG1hcDoxMjYgdGVsZXBob25lLWV2ZW50LzgwMDBcclxuYT1zc3JjOjIzMDQyMzI2ODcgY25hbWU6MjNEQllPbkgzYVMrTkdvWVxyXG5hPXNzcmM6MjMwNDIzMjY4NyBtc2lkOkhKWDZyUHZMRFhJdUp2a05OcDRBQ2QzWlJ3MjZLdjhmY2x5QiAxYWIyOTRjZC01ZTE5LTRmYjEtYWE3OS05YmVhNzU5MDIzMzFcclxubT12aWRlbyA0MTEzOCBVRFAvVExTL1JUUC9TQVZQRiA5NiA5NyAxMjcgMTIxIDEyNSAxMDcgMTA4IDEwOSAxMjQgMTIwIDEyMyAxMTkgMzUgMzYgNDEgNDIgOTggOTkgMTAwIDEwMSAxMTQgMTE1IDExNlxyXG5jPUlOIElQNCAxODkuMTIzLjYzLjU2XHJcbmE9cnRjcDo5IElOIElQNCAwLjAuMC4wXHJcbmE9Y2FuZGlkYXRlOjQxODIyOTAxMTcgMSB1ZHAgMjEyMjI2Mjc4MyAyODA0OjE0YzozZjg3OjgyODI6YzgxNjo5MDQ5OjQ0ZDoyN2EgNDE2MjcgdHlwIGhvc3QgZ2VuZXJhdGlvbiAwIG5ldHdvcmstaWQgMiBuZXR3b3JrLWNvc3QgMTBcclxuYT1jYW5kaWRhdGU6MTQzNDMwMTc4OCAxIHVkcCAyMTIyMTk0Njg3IDE5Mi4xNjguMC4xMCA0MTEzOCB0eXAgaG9zdCBnZW5lcmF0aW9uIDAgbmV0d29yay1pZCAxIG5ldHdvcmstY29zdCAxMFxyXG5hPWNhbmRpZGF0ZToyNzQxODgxOTkyIDEgdWRwIDE2ODU5ODcwNzEgMTg5LjEyMy42My41NiA0MTEzOCB0eXAgc3JmbHggcmFkZHIgMTkyLjE2OC4wLjEwIHJwb3J0IDQxMTM4IGdlbmVyYXRpb24gMCBuZXR3b3JrLWlkIDEgbmV0d29yay1jb3N0IDEwXHJcbmE9Y2FuZGlkYXRlOjMwODM1NTUzODEgMSB0Y3AgMTUxODI4MzAwNyAyODA0OjE0YzozZjg3OjgyODI6YzgxNjo5MDQ5OjQ0ZDoyN2EgOSB0eXAgaG9zdCB0Y3B0eXBlIGFjdGl2ZSBnZW5lcmF0aW9uIDAgbmV0d29yay1pZCAyIG5ldHdvcmstY29zdCAxMFxyXG5hPWNhbmRpZGF0ZTo0Njk2NDk4MzYgMSB0Y3AgMTUxODIxNDkxMSAxOTIuMTY4LjAuMTAgOSB0eXAgaG9zdCB0Y3B0eXBlIGFjdGl2ZSBnZW5lcmF0aW9uIDAgbmV0d29yay1pZCAxIG5ldHdvcmstY29zdCAxMFxyXG5hPWljZS11ZnJhZzp0OWNFXHJcbmE9aWNlLXB3ZDpHbXpMYVNmSFBwTDVEdkE2N213eHErR2JcclxuYT1pY2Utb3B0aW9uczp0cmlja2xlXHJcbmE9ZmluZ2VycHJpbnQ6c2hhLTI1NiBCNjpGODo4MDozQTpBMToyQjpFNzoyMTozNTpFNzo4NzoxQjpBMDo1NjowNzo2RDoyQTpCODoyNTpENDo2QjpDQjpFMTpGQTpFOTpFQjpFNjpGQTo1QjpCNjo1MDpFNFxyXG5hPXNldHVwOmFjdHBhc3NcclxuYT1taWQ6MVxyXG5hPWV4dG1hcDoxNCB1cm46aWV0ZjpwYXJhbXM6cnRwLWhkcmV4dDp0b2Zmc2V0XHJcbmE9ZXh0bWFwOjIgaHR0cDovL3d3dy53ZWJydGMub3JnL2V4cGVyaW1lbnRzL3J0cC1oZHJleHQvYWJzLXNlbmQtdGltZVxyXG5hPWV4dG1hcDoxMyB1cm46M2dwcDp2aWRlby1vcmllbnRhdGlvblxyXG5hPWV4dG1hcDozIGh0dHA6Ly93d3cuaWV0Zi5vcmcvaWQvZHJhZnQtaG9sbWVyLXJtY2F0LXRyYW5zcG9ydC13aWRlLWNjLWV4dGVuc2lvbnMtMDFcclxuYT1leHRtYXA6NSBodHRwOi8vd3d3LndlYnJ0Yy5vcmcvZXhwZXJpbWVudHMvcnRwLWhkcmV4dC9wbGF5b3V0LWRlbGF5XHJcbmE9ZXh0bWFwOjYgaHR0cDovL3d3dy53ZWJydGMub3JnL2V4cGVyaW1lbnRzL3J0cC1oZHJleHQvdmlkZW8tY29udGVudC10eXBlXHJcbmE9ZXh0bWFwOjcgaHR0cDovL3d3dy53ZWJydGMub3JnL2V4cGVyaW1lbnRzL3J0cC1oZHJleHQvdmlkZW8tdGltaW5nXHJcbmE9ZXh0bWFwOjggaHR0cDovL3d3dy53ZWJydGMub3JnL2V4cGVyaW1lbnRzL3J0cC1oZHJleHQvY29sb3Itc3BhY2VcclxuYT1leHRtYXA6NCB1cm46aWV0ZjpwYXJhbXM6cnRwLWhkcmV4dDpzZGVzOm1pZFxyXG5hPWV4dG1hcDoxMCB1cm46aWV0ZjpwYXJhbXM6cnRwLWhkcmV4dDpzZGVzOnJ0cC1zdHJlYW0taWRcclxuYT1leHRtYXA6MTEgdXJuOmlldGY6cGFyYW1zOnJ0cC1oZHJleHQ6c2RlczpyZXBhaXJlZC1ydHAtc3RyZWFtLWlkXHJcbmE9c2VuZHJlY3ZcclxuYT1tc2lkOkhKWDZyUHZMRFhJdUp2a05OcDRBQ2QzWlJ3MjZLdjhmY2x5QiA0ZmM1NGVlZC1jZWJkLTQ2ZGMtOThjOS03NjNlYzRlMDVmODdcclxuYT1ydGNwLW11eFxyXG5hPXJ0Y3AtcnNpemVcclxuYT1ydHBtYXA6OTYgVlA4LzkwMDAwXHJcbmE9cnRjcC1mYjo5NiBnb29nLXJlbWJcclxuYT1ydGNwLWZiOjk2IHRyYW5zcG9ydC1jY1xyXG5hPXJ0Y3AtZmI6OTYgY2NtIGZpclxyXG5hPXJ0Y3AtZmI6OTYgbmFja1xyXG5hPXJ0Y3AtZmI6OTYgbmFjayBwbGlcclxuYT1ydHBtYXA6OTcgcnR4LzkwMDAwXHJcbmE9Zm10cDo5NyBhcHQ9OTZcclxuYT1ydHBtYXA6MTI3IEgyNjQvOTAwMDBcclxuYT1ydGNwLWZiOjEyNyBnb29nLXJlbWJcclxuYT1ydGNwLWZiOjEyNyB0cmFuc3BvcnQtY2NcclxuYT1ydGNwLWZiOjEyNyBjY20gZmlyXHJcbmE9cnRjcC1mYjoxMjcgbmFja1xyXG5hPXJ0Y3AtZmI6MTI3IG5hY2sgcGxpXHJcbmE9Zm10cDoxMjcgbGV2ZWwtYXN5bW1ldHJ5LWFsbG93ZWQ9MTtwYWNrZXRpemF0aW9uLW1vZGU9MTtwcm9maWxlLWxldmVsLWlkPTQyMDAxZlxyXG5hPXJ0cG1hcDoxMjEgcnR4LzkwMDAwXHJcbmE9Zm10cDoxMjEgYXB0PTEyN1xyXG5hPXJ0cG1hcDoxMjUgSDI2NC85MDAwMFxyXG5hPXJ0Y3AtZmI6MTI1IGdvb2ctcmVtYlxyXG5hPXJ0Y3AtZmI6MTI1IHRyYW5zcG9ydC1jY1xyXG5hPXJ0Y3AtZmI6MTI1IGNjbSBmaXJcclxuYT1ydGNwLWZiOjEyNSBuYWNrXHJcbmE9cnRjcC1mYjoxMjUgbmFjayBwbGlcclxuYT1mbXRwOjEyNSBsZXZlbC1hc3ltbWV0cnktYWxsb3dlZD0xO3BhY2tldGl6YXRpb24tbW9kZT0wO3Byb2ZpbGUtbGV2ZWwtaWQ9NDIwMDFmXHJcbmE9cnRwbWFwOjEwNyBydHgvOTAwMDBcclxuYT1mbXRwOjEwNyBhcHQ9MTI1XHJcbmE9cnRwbWFwOjEwOCBIMjY0LzkwMDAwXHJcbmE9cnRjcC1mYjoxMDggZ29vZy1yZW1iXHJcbmE9cnRjcC1mYjoxMDggdHJhbnNwb3J0LWNjXHJcbmE9cnRjcC1mYjoxMDggY2NtIGZpclxyXG5hPXJ0Y3AtZmI6MTA4IG5hY2tcclxuYT1ydGNwLWZiOjEwOCBuYWNrIHBsaVxyXG5hPWZtdHA6MTA4IGxldmVsLWFzeW1tZXRyeS1hbGxvd2VkPTE7cGFja2V0aXphdGlvbi1tb2RlPTE7cHJvZmlsZS1sZXZlbC1pZD00MmUwMWZcclxuYT1ydHBtYXA6MTA5IHJ0eC85MDAwMFxyXG5hPWZtdHA6MTA5IGFwdD0xMDhcclxuYT1ydHBtYXA6MTI0IEgyNjQvOTAwMDBcclxuYT1ydGNwLWZiOjEyNCBnb29nLXJlbWJcclxuYT1ydGNwLWZiOjEyNCB0cmFuc3BvcnQtY2NcclxuYT1ydGNwLWZiOjEyNCBjY20gZmlyXHJcbmE9cnRjcC1mYjoxMjQgbmFja1xyXG5hPXJ0Y3AtZmI6MTI0IG5hY2sgcGxpXHJcbmE9Zm10cDoxMjQgbGV2ZWwtYXN5bW1ldHJ5LWFsbG93ZWQ9MTtwYWNrZXRpemF0aW9uLW1vZGU9MDtwcm9maWxlLWxldmVsLWlkPTQyZTAxZlxyXG5hPXJ0cG1hcDoxMjAgcnR4LzkwMDAwXHJcbmE9Zm10cDoxMjAgYXB0PTEyNFxyXG5hPXJ0cG1hcDoxMjMgSDI2NC85MDAwMFxyXG5hPXJ0Y3AtZmI6MTIzIGdvb2ctcmVtYlxyXG5hPXJ0Y3AtZmI6MTIzIHRyYW5zcG9ydC1jY1xyXG5hPXJ0Y3AtZmI6MTIzIGNjbSBmaXJcclxuYT1ydGNwLWZiOjEyMyBuYWNrXHJcbmE9cnRjcC1mYjoxMjMgbmFjayBwbGlcclxuYT1mbXRwOjEyMyBsZXZlbC1hc3ltbWV0cnktYWxsb3dlZD0xO3BhY2tldGl6YXRpb24tbW9kZT0xO3Byb2ZpbGUtbGV2ZWwtaWQ9NGQwMDFmXHJcbmE9cnRwbWFwOjExOSBydHgvOTAwMDBcclxuYT1mbXRwOjExOSBhcHQ9MTIzXHJcbmE9cnRwbWFwOjM1IEgyNjQvOTAwMDBcclxuYT1ydGNwLWZiOjM1IGdvb2ctcmVtYlxyXG5hPXJ0Y3AtZmI6MzUgdHJhbnNwb3J0LWNjXHJcbmE9cnRjcC1mYjozNSBjY20gZmlyXHJcbmE9cnRjcC1mYjozNSBuYWNrXHJcbmE9cnRjcC1mYjozNSBuYWNrIHBsaVxyXG5hPWZtdHA6MzUgbGV2ZWwtYXN5bW1ldHJ5LWFsbG93ZWQ9MTtwYWNrZXRpemF0aW9uLW1vZGU9MDtwcm9maWxlLWxldmVsLWlkPTRkMDAxZlxyXG5hPXJ0cG1hcDozNiBydHgvOTAwMDBcclxuYT1mbXRwOjM2IGFwdD0zNVxyXG5hPXJ0cG1hcDo0MSBBVjEvOTAwMDBcclxuYT1ydGNwLWZiOjQxIGdvb2ctcmVtYlxyXG5hPXJ0Y3AtZmI6NDEgdHJhbnNwb3J0LWNjXHJcbmE9cnRjcC1mYjo0MSBjY20gZmlyXHJcbmE9cnRjcC1mYjo0MSBuYWNrXHJcbmE9cnRjcC1mYjo0MSBuYWNrIHBsaVxyXG5hPXJ0cG1hcDo0MiBydHgvOTAwMDBcclxuYT1mbXRwOjQyIGFwdD00MVxyXG5hPXJ0cG1hcDo5OCBWUDkvOTAwMDBcclxuYT1ydGNwLWZiOjk4IGdvb2ctcmVtYlxyXG5hPXJ0Y3AtZmI6OTggdHJhbnNwb3J0LWNjXHJcbmE9cnRjcC1mYjo5OCBjY20gZmlyXHJcbmE9cnRjcC1mYjo5OCBuYWNrXHJcbmE9cnRjcC1mYjo5OCBuYWNrIHBsaVxyXG5hPWZtdHA6OTggcHJvZmlsZS1pZD0wXHJcbmE9cnRwbWFwOjk5IHJ0eC85MDAwMFxyXG5hPWZtdHA6OTkgYXB0PTk4XHJcbmE9cnRwbWFwOjEwMCBWUDkvOTAwMDBcclxuYT1ydGNwLWZiOjEwMCBnb29nLXJlbWJcclxuYT1ydGNwLWZiOjEwMCB0cmFuc3BvcnQtY2NcclxuYT1ydGNwLWZiOjEwMCBjY20gZmlyXHJcbmE9cnRjcC1mYjoxMDAgbmFja1xyXG5hPXJ0Y3AtZmI6MTAwIG5hY2sgcGxpXHJcbmE9Zm10cDoxMDAgcHJvZmlsZS1pZD0yXHJcbmE9cnRwbWFwOjEwMSBydHgvOTAwMDBcclxuYT1mbXRwOjEwMSBhcHQ9MTAwXHJcbmE9cnRwbWFwOjExNCByZWQvOTAwMDBcclxuYT1ydHBtYXA6MTE1IHJ0eC85MDAwMFxyXG5hPWZtdHA6MTE1IGFwdD0xMTRcclxuYT1ydHBtYXA6MTE2IHVscGZlYy85MDAwMFxyXG5hPXNzcmMtZ3JvdXA6RklEIDE1Njg1NDcxNTEgMTAwMjEwMjUyOVxyXG5hPXNzcmM6MTU2ODU0NzE1MSBjbmFtZToyM0RCWU9uSDNhUytOR29ZXHJcbmE9c3NyYzoxNTY4NTQ3MTUxIG1zaWQ6SEpYNnJQdkxEWEl1SnZrTk5wNEFDZDNaUncyNkt2OGZjbHlCIDRmYzU0ZWVkLWNlYmQtNDZkYy05OGM5LTc2M2VjNGUwNWY4N1xyXG5hPXNzcmM6MTAwMjEwMjUyOSBjbmFtZToyM0RCWU9uSDNhUytOR29ZXHJcbmE9c3NyYzoxMDAyMTAyNTI5IG1zaWQ6SEpYNnJQdkxEWEl1SnZrTk5wNEFDZDNaUncyNkt2OGZjbHlCIDRmYzU0ZWVkLWNlYmQtNDZkYy05OGM5LTc2M2VjNGUwNWY4N1xyXG4ifQ==", &offer)

	// Set the remote SessionDescription
	if err = peerConnection.SetRemoteDescription(offer); err != nil {
		panic(err)
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		panic(err)
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	// Output the answer in base64 so we can paste it in browser
	fmt.Println(signal.Encode(*peerConnection.LocalDescription()))

	// Block forever
	select {}
}
