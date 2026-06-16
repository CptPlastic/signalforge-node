//go:build darwin

package sdr

import (
	"os/exec"
	"regexp"
	"strings"
)

var iokitLine = regexp.MustCompile(`"USB Product Name"\s*=\s*"([^"]+)"`)

func listUSBDevices() ([]usbDeviceEntry, error) {
	out, err := exec.Command("system_profiler", "SPUSBDataType").Output()
	if err != nil {
		return enumerateFromIORegistry()
	}
	return parseSystemProfiler(string(out)), nil
}

func enumerateFromIORegistry() ([]usbDeviceEntry, error) {
	out, err := exec.Command("ioreg", "-p", "IOUSB", "-l").Output()
	if err != nil {
		return nil, err
	}
	return parseIOReg(string(out)), nil
}

func parseSystemProfiler(text string) []usbDeviceEntry {
	var devices []usbDeviceEntry
	blocks := strings.Split(text, "\n\n")
	for _, block := range blocks {
		lower := strings.ToLower(block)
		if !strings.Contains(lower, "rtl") && !strings.Contains(lower, "2838") && !strings.Contains(lower, "2832") {
			continue
		}
		if !strings.Contains(lower, "vendor id") {
			continue
		}
		devices = append(devices, usbDeviceEntry{
			VendorID: 0x0bda,
			ProductID: 0x2838,
			Product:  "RTL-SDR",
			Serial:   extractField(block, "Serial Number"),
		})
	}
	return devices
}

func parseIOReg(text string) []usbDeviceEntry {
	var devices []usbDeviceEntry
	lines := strings.Split(text, "\n")
	var current usbDeviceEntry
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "+-o") || strings.HasPrefix(trim, "|") {
			if current.VendorID == 0x0bda && (current.ProductID == 0x2838 || current.ProductID == 0x2832) {
				devices = append(devices, current)
			}
			current = usbDeviceEntry{}
		}
		if strings.Contains(trim, `"idVendor"`) && strings.Contains(trim, "0x0bda") {
			current.VendorID = 0x0bda
		}
		if strings.Contains(trim, `"idProduct"`) {
			if strings.Contains(trim, "0x2838") {
				current.ProductID = 0x2838
			}
			if strings.Contains(trim, "0x2832") {
				current.ProductID = 0x2832
			}
		}
		if strings.Contains(trim, `"USB Serial Number"`) {
			current.Serial = extractQuoted(trim)
		}
		if m := iokitLine.FindStringSubmatch(trim); len(m) == 2 {
			current.Product = m[1]
		}
	}
	if current.VendorID == 0x0bda && (current.ProductID == 0x2838 || current.ProductID == 0x2832) {
		devices = append(devices, current)
	}
	return devices
}

func extractField(block, label string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, label+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func extractQuoted(line string) string {
	start := strings.Index(line, `"`)
	if start < 0 {
		return ""
	}
	rest := line[start+1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
