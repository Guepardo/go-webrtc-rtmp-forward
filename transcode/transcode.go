package transcode

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"time"

	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
)

type Transcode struct {
	AudioWriter    webm.BlockWriteCloser
	AudioBuilder   *samplebuilder.SampleBuilder
	AudioTimestamp time.Duration

	VideoWriter    webm.BlockWriteCloser
	VideoBuilder   *samplebuilder.SampleBuilder
	VideoTimestamp time.Duration

	RtmpUrlWithStreamKey string
}

func crazyVP8CodecHeaderUnpacker(sample *media.Sample) (int, int) {
	raw := uint(sample.Data[6]) | uint(sample.Data[7])<<8 | uint(sample.Data[8])<<16 | uint(sample.Data[9])<<24
	width := int(raw & 0x3FFF)
	height := int((raw >> 16) & 0x3FFF)

	return width, height
}

// maxLate is how long to wait until we can construct a completed media.Sample.
// maxLate is measured in RTP packet sequence numbers.
// A large maxLate will result in less packet loss but higher latency.
const MAX_LATE = 10

// Public

func (transcode *Transcode) Initialize(videoClockRate, audioClockRate int, rtmpUrlWithStreamKey string) {
	transcode.VideoBuilder = samplebuilder.New(MAX_LATE, &codecs.VP8Packet{}, uint32(videoClockRate))
	transcode.AudioBuilder = samplebuilder.New(MAX_LATE, &codecs.OpusPacket{}, uint32(audioClockRate))

	transcode.RtmpUrlWithStreamKey = rtmpUrlWithStreamKey
}

func (transcode *Transcode) HandleRTPPacket(packet *rtp.Packet, codecType webrtc.RTPCodecType) {
	switch codecType {
	case webrtc.RTPCodecTypeAudio:
		transcode.handleAudioPacket(packet)
	case webrtc.RTPCodecTypeVideo:
		transcode.handleVideoPacket(packet)
	}

}

// Private

func (transcode *Transcode) handleAudioPacket(packet *rtp.Packet) {
	transcode.AudioBuilder.Push(packet)

	for {
		sample := transcode.AudioBuilder.Pop()
		if sample == nil {
			return
		}
		if transcode.AudioWriter != nil {
			transcode.AudioTimestamp += sample.Duration
			timestamp := int64(transcode.AudioTimestamp / time.Millisecond)

			if _, err := transcode.AudioWriter.Write(true, timestamp, sample.Data); err != nil {
				panic(err)
			}
		}
	}
}

func (transcode *Transcode) handleVideoPacket(packet *rtp.Packet) {
	transcode.VideoBuilder.Push(packet)

	for {
		sample := transcode.VideoBuilder.Pop()

		if sample == nil {
			return
		}
		// Read VP8 header and ask if it is a videoKeyFrame.
		videoKeyframe := (sample.Data[0]&0x1 == 0)

		if videoKeyframe {
			width, height := crazyVP8CodecHeaderUnpacker(sample)

			if transcode.VideoWriter == nil || transcode.AudioWriter == nil {
				// Initialize WebM saver using received frame size.
				transcode.startFFmpeg(width, height)
			}
		}

		if transcode.VideoWriter != nil {
			transcode.VideoTimestamp += sample.Duration
			timestamp := int64(transcode.AudioTimestamp / time.Millisecond)

			if _, err := transcode.VideoWriter.Write(videoKeyframe, timestamp, sample.Data); err != nil {
				panic(err)
			}
		}
	}
}

func (transcode *Transcode) setUpCodecsAndOpenWriters(width, height int, ffmpegIn io.WriteCloser) {
	codecsSpec := []webm.TrackEntry{
		{
			Name:            "Audio",
			TrackNumber:     1,
			TrackUID:        12345,
			CodecID:         "A_OPUS",
			TrackType:       2,
			DefaultDuration: 20000000,
			Audio: &webm.Audio{
				SamplingFrequency: 48000.0,
				Channels:          2,
			},
		}, {
			Name:            "Video",
			TrackNumber:     2,
			TrackUID:        67890,
			CodecID:         "V_VP8",
			TrackType:       1,
			DefaultDuration: 33333333,
			Video: &webm.Video{
				PixelWidth:  uint64(width),
				PixelHeight: uint64(height),
			},
		},
	}

	blockWriteCloserArray, err := webm.NewSimpleBlockWriter(ffmpegIn, codecsSpec)

	if err != nil {
		panic(err)
	}

	log.Printf("WebM saver has started with video width=%d, height=%d\n", width, height)

	transcode.AudioWriter = blockWriteCloserArray[0]
	transcode.VideoWriter = blockWriteCloserArray[1]

	log.Println("Writers Set up")
}

func (transcode *Transcode) startFFmpeg(width, height int) {
	// Create a ffmpeg process that consumes MKV via stdin, and broadcasts out to rtmp url provided
	// https://stackoverflow.com/questions/16658873/how-to-minimize-the-delay-in-a-live-streaming-with-ffmpeg/49273163#49273163
	ffmpeg := exec.Command("ffmpeg", "-re", "-i", "pipe:0", "-c:v", "libx264", "-preset", "veryfast", "-b:v", "3000k", "-maxrate", "3000k", "-bufsize", "6000k", "-pix_fmt", "yuv420p", "-g", "50", "-c:a", "aac", "-b:a", "160k", "-ac", "2", "-ar", "44100", "-f", "flv", transcode.RtmpUrlWithStreamKey, "-loglevel", "debug") //nolint
	ffmpegIn, _ := ffmpeg.StdinPipe()
	ffmpegOut, _ := ffmpeg.StderrPipe()

	if err := ffmpeg.Start(); err != nil {
		panic(err)
	}

	go func() {
		scanner := bufio.NewScanner(ffmpegOut)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()

	transcode.setUpCodecsAndOpenWriters(width, height, ffmpegIn)
}
