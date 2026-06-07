package recorder

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

const MinCanaryInterval = 30 * time.Second

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
	} else {
		meta.TalkgroupLabel = "CANARY"
	}
	return meta
}

// CanaryWAV returns a short audible two-tone pip so the heartbeat is obvious on the hub.
func CanaryWAV() ([]byte, time.Duration) {
	const sampleRate = 16000
	const channels = 1
	tones := []struct {
		frequency float64
		duration  time.Duration
	}{
		{880, 180 * time.Millisecond},
		{660, 180 * time.Millisecond},
		{880, 220 * time.Millisecond},
	}
	return toneSequenceWAV(sampleRate, channels, tones), 580 * time.Millisecond
}

func toneSequenceWAV(sampleRate, channels int, tones []struct {
	frequency float64
	duration  time.Duration
}) []byte {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if channels <= 0 {
		channels = 1
	}

	samples := make([]int16, 0)
	for _, tone := range tones {
		sampleCount := int(float64(sampleRate) * tone.duration.Seconds())
		if sampleCount <= 0 {
			continue
		}
		for i := 0; i < sampleCount; i++ {
			t := float64(i) / float64(sampleRate)
			envelope := math.Min(1, math.Min(float64(i)/200, float64(sampleCount-i)/200))
			value := math.Sin(2*math.Pi*tone.frequency*t) * envelope * 0.35
			samples = append(samples, int16(value*math.MaxInt16))
		}
		gap := int(float64(sampleRate) * 0.04)
		for i := 0; i < gap; i++ {
			samples = append(samples, 0)
		}
	}

	dataSize := len(samples) * channels * 2
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
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(sample))
	}
	return buf
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

func NormalizeCanaryInterval(interval time.Duration) time.Duration {
	if interval < MinCanaryInterval {
		return MinCanaryInterval
	}
	return interval
}

func IsCanaryHeartbeatFile(name string) bool {
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	return strings.HasPrefix(stem, "canary-")
}

func CanaryAudioName(now time.Time) string {
	return fmt.Sprintf("canary-%d.wav", now.UnixNano())
}
