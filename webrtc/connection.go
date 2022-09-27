package webrtc

import (
	"github.com/go-webrtc-rtmp-forward/transcode"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v3"
)

const (
	VIDEO_CLOCK_RATE = 90000
	AUDIO_CLOCK_RATE = 48000
)

func createMediaEngine() *webrtc.MediaEngine {
	// Everything below is the Pion WebRTC API! Thanks for using it ❤️.
	// Create a MediaEngine object to configure the supported codec
	mediaEngine := &webrtc.MediaEngine{}

	videoCodecParameters := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeVP8,
			ClockRate:    VIDEO_CLOCK_RATE,
			Channels:     0,
			SDPFmtpLine:  "",
			RTCPFeedback: nil,
		},
	}
	// Setup the codecs you want to use.
	// We'll use a VP8 and Opus but you can also define your own
	if err := mediaEngine.RegisterCodec(videoCodecParameters, webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}

	audioCodecCodecParameters := webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:     webrtc.MimeTypeOpus,
			ClockRate:    AUDIO_CLOCK_RATE,
			Channels:     0,
			SDPFmtpLine:  "",
			RTCPFeedback: nil,
		},
	}

	if err := mediaEngine.RegisterCodec(audioCodecCodecParameters, webrtc.RTPCodecTypeAudio); err != nil {
		panic(err)
	}

	return mediaEngine
}

func createNewApiWithMediaEngine(mediaEngine *webrtc.MediaEngine) *webrtc.API {
	// Create a InterceptorRegistry. This is the user configurable RTP/RTCP Pipeline.
	// This provides NACKs, RTCP Reports and other features. If you use `webrtc.NewPeerConnection`
	// this is enabled by default. If you are manually managing You MUST create a InterceptorRegistry
	// for each PeerConnection.
	interceptorRegistry := &interceptor.Registry{}

	// Use the default set of Interceptors
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		panic(err)
	}

	// Create the API object with the MediaEngine
	return webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine), webrtc.WithInterceptorRegistry(interceptorRegistry))
}

func CreatePeerConnection(sessionDescriptionOffer, peerId, rtmpUrlWithStreamKey string, peerEventChan chan PeerEvent) *webrtc.PeerConnection {
	mediaEngine := createMediaEngine()

	// Create the API object with the MediaEngine
	api := createNewApiWithMediaEngine(mediaEngine)

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

	// TODO: move this code to PeerManager
	// defer func() {
	// 	if cErr := peerConnection.Close(); cErr != nil {
	// 		fmt.Printf("cannot close peerConnection: %v\n", cErr)
	// 	}
	// }()

	// Allow us to receive 1 audio track, and 1 video track
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
		panic(err)
	} else if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		panic(err)
	}

	udpForwarder := &transcode.UdpForwarder{}

	udpForwarder.Initialize(4000, 4002)

	peerLifeCycleManager := PeerLifeCycleManager{
		PeerId:         peerId,
		PeerEventChan:  peerEventChan,
		PeerConnection: peerConnection,
		UdpForwarder:   udpForwarder,
	}

	// Set a handler for when a new remote track starts, this handler will forward data to
	// our UDP listeners.
	// In your application this is where you would handle/process audio/video
	peerConnection.OnTrack(peerLifeCycleManager.OnTrack)

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(peerLifeCycleManager.OnICEConnectionStateChange)

	// Set the handler for Peer connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnConnectionStateChange(peerLifeCycleManager.OnConnectionStateChange)

	// Wait for the offer to be pasted
	offer := webrtc.SessionDescription{}

	Decode(sessionDescriptionOffer, &offer)

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

	return peerConnection
}
