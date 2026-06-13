package api

import "testing"

func TestArchiveAudioExtension(t *testing.T) {
	tests := []struct {
		name      string
		audioName string
		audioType string
		want      string
	}{
		{name: "from filename", audioName: "clip.mp3", audioType: "audio/mpeg", want: ".mp3"},
		{name: "mpeg type", audioName: "", audioType: "audio/mpeg", want: ".mp3"},
		{name: "m4a type", audioName: "", audioType: "audio/mp4", want: ".m4a"},
		{name: "webm ptt", audioName: "ptt.webm", audioType: "audio/webm;codecs=opus", want: ".webm"},
		{name: "fallback", audioName: "", audioType: "application/octet-stream", want: ".bin"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := archiveAudioExtension(tc.audioName, tc.audioType); got != tc.want {
				t.Fatalf("archiveAudioExtension(%q, %q) = %q, want %q", tc.audioName, tc.audioType, got, tc.want)
			}
		})
	}
}
