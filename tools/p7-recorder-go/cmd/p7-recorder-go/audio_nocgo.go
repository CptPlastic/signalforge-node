//go:build !cgo

package main

import (
	"errors"
	"fmt"
	"io"
)

type AudioContext struct{}

func newAudioContext() (*AudioContext, error) {
	return &AudioContext{}, nil
}

func (a *AudioContext) Close() {}

func (a *AudioContext) ListDevices(w io.Writer) error {
	fmt.Fprintln(w, "audio capture is unavailable because this binary was built with CGO_ENABLED=0")
	return nil
}

func (a *AudioContext) StartCapture(_ AudioConfig, _ chan<- []byte) (func() error, error) {
	return nil, errors.New("audio capture is unavailable because this binary was built with CGO_ENABLED=0")
}
