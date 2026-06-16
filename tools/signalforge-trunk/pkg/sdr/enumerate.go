//go:build linux || darwin || windows

package sdr

import (
	"fmt"
	"sort"
)

// RTL-SDR USB identifiers (Nooelec v5 and common clones use 0x0bda:0x2838).
var rtlSDRUSBIDs = []struct {
	vid, pid uint16
}{
	{0x0bda, 0x2832},
	{0x0bda, 0x2838},
}

func enumerateRTLSDR() ([]Device, error) {
	raw, err := listUSBDevices()
	if err != nil {
		return nil, err
	}
	var devices []Device
	index := 0
	for _, entry := range raw {
		if !isRTLSDR(entry.VendorID, entry.ProductID) {
			continue
		}
		id := fmt.Sprintf("rtl-sdr-%d", index)
		serial := entry.Serial
		if serial == "" {
			serial = fmt.Sprintf("bus%d-addr%d", entry.Bus, entry.Address)
		}
		devices = append(devices, Device{
			ID:           id,
			Index:        index,
			Serial:       serial,
			VendorID:     entry.VendorID,
			ProductID:    entry.ProductID,
			Manufacturer: entry.Manufacturer,
			Product:      entry.Product,
		})
		index++
	}
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Serial < devices[j].Serial
	})
	for i := range devices {
		devices[i].Index = i
		devices[i].ID = fmt.Sprintf("rtl-sdr-%d", i)
	}
	return devices, nil
}

func isRTLSDR(vid, pid uint16) bool {
	for _, id := range rtlSDRUSBIDs {
		if id.vid == vid && id.pid == pid {
			return true
		}
	}
	return false
}

type usbDeviceEntry struct {
	VendorID     uint16
	ProductID    uint16
	Serial       string
	Manufacturer string
	Product      string
	Bus          int
	Address      int
}
