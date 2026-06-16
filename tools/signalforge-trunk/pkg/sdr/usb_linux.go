//go:build linux

package sdr

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func listUSBDevices() ([]usbDeviceEntry, error) {
	const usbRoot = "/sys/bus/usb/devices"
	entries, err := os.ReadDir(usbRoot)
	if err != nil {
		return nil, err
	}
	var devices []usbDeviceEntry
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ":") {
			continue
		}
		base := filepath.Join(usbRoot, entry.Name())
		vid, okVID := readHexID(filepath.Join(base, "idVendor"))
		pid, okPID := readHexID(filepath.Join(base, "idProduct"))
		if !okVID || !okPID {
			continue
		}
		devices = append(devices, usbDeviceEntry{
			VendorID:     vid,
			ProductID:    pid,
			Serial:       readString(filepath.Join(base, "serial")),
			Manufacturer: readString(filepath.Join(base, "manufacturer")),
			Product:      readString(filepath.Join(base, "product")),
		})
	}
	return devices, nil
}

func readHexID(path string) (uint16, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
