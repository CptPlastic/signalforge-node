package api

import (
	"bytes"
	_ "embed"
	"net/http"
	"strings"
	"time"
)

// Square station art rendered from client/public/pwa/icon.svg.
// logo.png is 1024×1024. logo-512.png is the same mark at 512×512.
//
//go:embed streamart/logo-1024.png
var streamLogo1024 []byte

//go:embed streamart/logo-512.png
var streamLogo512 []byte

// streamLogoModTime is stable so clients can cache the artwork.
var streamLogoModTime = time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)

func streamLogoPNG(name string) ([]byte, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "logo.png", "logo-1024.png":
		return streamLogo1024, true
	case "logo-512.png":
		return streamLogo512, true
	default:
		return nil, false
	}
}

func serveStreamLogo(w http.ResponseWriter, r *http.Request, name string, png []byte) {
	header := w.Header()
	header.Set("Content-Type", "image/png")
	header.Set("Cache-Control", "public, max-age=86400")
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, streamLogoModTime, bytes.NewReader(png))
}

func publicStreamLogoURL(base string) string {
	const path = "/public/stream/logo.png"
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return path
	}
	return base + path
}
