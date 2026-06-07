package recorder

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

type CanarySettings struct {
	Enabled        bool
	Interval       time.Duration
	Talkgroup      int
	TalkgroupLabel string
}

func (s Settings) CanaryMetadata() Metadata {
	meta := s.Metadata
	if s.Canary.Talkgroup > 0 {
		meta.Talkgroup = s.Canary.Talkgroup
	}
	if strings.TrimSpace(s.Canary.TalkgroupLabel) != "" {
		meta.TalkgroupLabel = s.Canary.TalkgroupLabel
	}
	return meta
}

// SilentWAV returns a 1-second mono 16-bit PCM WAV at the given sample rate.
func SilentWAV(sampleRate, channels int) []byte {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	dataSize := sampleRate * channels * 2
	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	byteRate := sampleRate * channels * 2
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	return buf
}

func CanaryAudioName(now time.Time) string {
	return fmt.Sprintf("canary-%d.wav", now.Unix())
}
