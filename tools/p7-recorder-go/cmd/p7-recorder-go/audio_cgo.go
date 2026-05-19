//go:build cgo

package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gen2brain/malgo"
)

type AudioContext struct {
	ctx *malgo.AllocatedContext
}

func newAudioContext() (*AudioContext, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		if os.Getenv("P7_RECORDER_AUDIO_DEBUG") == "1" {
			fmt.Fprintf(os.Stderr, "audio: %s\n", message)
		}
	})
	if err != nil {
		return nil, err
	}
	return &AudioContext{ctx: ctx}, nil
}

func (a *AudioContext) Close() {
	if a != nil && a.ctx != nil {
		_ = a.ctx.Uninit()
		a.ctx.Free()
	}
}

func (a *AudioContext) ListDevices(w io.Writer) error {
	devices, err := a.ctx.Devices(malgo.Capture)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Fprintln(w, "no capture devices found")
		return nil
	}
	for index, device := range devices {
		fmt.Fprintf(w, "%d\t%s\n", index, device.Name())
	}
	return nil
}

func (a *AudioContext) StartCapture(audio AudioConfig, out chan<- []byte) (func() error, error) {
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = uint32(audio.Channels)
	deviceConfig.SampleRate = uint32(audio.SampleRate)
	deviceConfig.PeriodSizeInFrames = uint32(max(1, audio.SampleRate*audio.BlockMS/1000))
	deviceConfig.Alsa.NoMMap = 1

	selectedDevice, err := a.findCaptureDevice(audio.Device)
	if err != nil {
		return nil, err
	}
	if selectedDevice != nil {
		deviceConfig.Capture.DeviceID = selectedDevice.ID.Pointer()
	}

	callbacks := malgo.DeviceCallbacks{
		Data: func(_, input []byte, _ uint32) {
			if len(input) == 0 {
				return
			}
			block := append([]byte(nil), input...)
			select {
			case out <- block:
			default:
				fmt.Fprintln(os.Stderr, "audio queue full; dropping block")
			}
		},
	}
	device, err := malgo.InitDevice(a.ctx.Context, deviceConfig, callbacks)
	if err != nil {
		return nil, err
	}
	if err := device.Start(); err != nil {
		device.Uninit()
		return nil, err
	}
	return func() error {
		err := device.Stop()
		device.Uninit()
		return err
	}, nil
}

func (a *AudioContext) findCaptureDevice(selection string) (*malgo.DeviceInfo, error) {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return nil, nil
	}
	devices, err := a.ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	if index, err := strconv.Atoi(selection); err == nil {
		if index < 0 || index >= len(devices) {
			return nil, fmt.Errorf("capture device index %d out of range", index)
		}
		return &devices[index], nil
	}
	needle := strings.ToLower(selection)
	for i := range devices {
		if strings.Contains(strings.ToLower(devices[i].Name()), needle) {
			return &devices[i], nil
		}
	}
	return nil, fmt.Errorf("capture device %q not found", selection)
}
