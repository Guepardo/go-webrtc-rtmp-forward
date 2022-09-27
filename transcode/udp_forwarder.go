package transcode

import (
	"errors"
	"fmt"
	"log"
	"net"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

type udpConn struct {
	conn        *net.UDPConn
	port        int
	payloadType uint8
	buffer      []byte
	rtpPacket   *rtp.Packet
}

type UdpForwarder struct {
	udpConns map[string]*udpConn
}

const BUFFER_SIZE = 1500

func (forwarder *UdpForwarder) Initialize(audioPort, videoPort int) {
	forwarder.udpConns = map[string]*udpConn{
		"audio": {
			port:        audioPort,
			payloadType: 111,
			buffer:      make([]byte, BUFFER_SIZE),
			rtpPacket:   &rtp.Packet{},
		},
		"video": {
			port:        videoPort,
			payloadType: 96,
			buffer:      make([]byte, BUFFER_SIZE),
			rtpPacket:   &rtp.Packet{},
		},
	}

	forwarder.openUdpConns()
}

func (forwarder *UdpForwarder) HandleTrack(track *webrtc.TrackRemote) {
	kind := track.Kind().String()
	conn := forwarder.udpConns[kind]

	buffer := conn.buffer
	rtpPacket := conn.rtpPacket

	n, _, err := track.Read(buffer)

	if err != nil {
		panic(err)
	}

	// Unmarshal the packet and update the PayloadType
	if err := rtpPacket.Unmarshal(buffer[:n]); err != nil {
		panic(err)
	}

	rtpPacket.PayloadType = conn.payloadType

	// Marshal into original buffer with updated PayloadType
	if n, err = rtpPacket.MarshalTo(buffer); err != nil {
		panic(err)
	}

	forwarder.connWrite(conn.conn, buffer[:n])
}

// Private

func (forwarder *UdpForwarder) connWrite(conn *net.UDPConn, buffer []byte) {
	if _, writeErr := conn.Write(buffer); writeErr != nil {
		// For this particular example, third party applications usually timeout after a short
		// amount of time during which the user doesn't have enough time to provide the answer
		// to the browser.
		// That's why, for this particular example, the user first needs to provide the answer
		// to the browser then open the third party application. Therefore we must not kill
		// the forward on "connection refused" errors
		var opError *net.OpError

		if errors.As(writeErr, &opError) && opError.Err.Error() != "write: connection refused" {
			panic(writeErr)
		}

		log.Println("write: connection refused")
	}
}

func (forwarder *UdpForwarder) openUdpConns() {
	// Create a local addr
	var localAddress *net.UDPAddr
	var err error

	if localAddress, err = net.ResolveUDPAddr("udp", "127.0.0.1:"); err != nil {
		panic(err)
	}

	for _, c := range forwarder.udpConns {
		var remoteAddress *net.UDPAddr

		if remoteAddress, err = net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", c.port)); err != nil {
			panic(err)
		}

		// Dial udp
		if c.conn, err = net.DialUDP("udp", localAddress, remoteAddress); err != nil {
			panic(err)
		}
	}
}
