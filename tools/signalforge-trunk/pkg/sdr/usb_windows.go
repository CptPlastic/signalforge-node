//go:build windows

package sdr

import (
	"os/exec"
	"strings"
)

func listUSBDevices() ([]usbDeviceEntry, error) {
	out, err := exec.Command("wmic", "path", "Win32_PnPEntity", "get", "DeviceID,Name", "/format:csv").Output()
	if err != nil {
		return enumerateFromPowerShell()
	}
	return parseWMIC(string(out)), nil
}

func enumerateFromPowerShell() ([]usbDeviceEntry, error) {
	script := `Get-PnpDevice -PresentOnly | Where-Object { $_.InstanceId -match 'VID_0BDA&PID_2838|VID_0BDA&PID_2832' } | Select-Object FriendlyName, InstanceId`
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return nil, err
	}
	var devices []usbDeviceEntry
	for _, line := range strings.Split(string(out), "\n") {
		upper := strings.ToUpper(line)
		if !strings.Contains(upper, "VID_0BDA") {
			continue
		}
		pid := uint16(0x2838)
		if strings.Contains(upper, "PID_2832") {
			pid = 0x2832
		}
		devices = append(devices, usbDeviceEntry{
			VendorID:  0x0bda,
			ProductID: pid,
			Product:   strings.TrimSpace(line),
		})
	}
	return devices, nil
}

func parseWMIC(text string) []usbDeviceEntry {
	var devices []usbDeviceEntry
	for _, line := range strings.Split(text, "\n") {
		upper := strings.ToUpper(line)
		if !strings.Contains(upper, "VID_0BDA") {
			continue
		}
		pid := uint16(0x2838)
		if strings.Contains(upper, "PID_2832") {
			pid = 0x2832
		}
		parts := strings.Split(line, ",")
		name := ""
		if len(parts) >= 2 {
			name = strings.TrimSpace(parts[len(parts)-1])
		}
		devices = append(devices, usbDeviceEntry{
			VendorID:  0x0bda,
			ProductID: pid,
			Product:   name,
		})
	}
	return devices
}
