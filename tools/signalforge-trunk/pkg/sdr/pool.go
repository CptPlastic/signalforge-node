package sdr

import (
	"fmt"
	"sort"
	"sync"
)

// Role identifies how a dongle is used in the scanner pool.
type Role string

const (
	RoleControlHunt Role = "control_hunt"
	RoleVoice       Role = "voice"
	RoleGMRS        Role = "gmrs"
	RoleHuntBackup  Role = "hunt_backup"
	RoleUnassigned  Role = "unassigned"
)

// Device is a discovered RTL-SDR dongle.
type Device struct {
	ID         string
	Index      int
	Serial     string
	VendorID   uint16
	ProductID  uint16
	Manufacturer string
	Product      string
	Role       Role
	Assigned   bool
}

// Pool manages dynamic SDR enumeration and role assignment.
type Pool struct {
	mu      sync.RWMutex
	devices []Device
	roles   RolePlan
}

type RolePlan struct {
	ControlHunt int
	Voice       int
	GMRS        int
	HuntBackup  int
}

func NewPool() *Pool {
	return &Pool{}
}

// Discover enumerates attached RTL-SDR devices. Replaces the device list.
func (p *Pool) Discover() ([]Device, error) {
	devices, err := enumerateRTLSDR()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.devices = devices
	p.rebalanceLocked()
	return append([]Device(nil), p.devices...), nil
}

// Devices returns a snapshot of the current pool.
func (p *Pool) Devices() []Device {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Device, len(p.devices))
	copy(out, p.devices)
	return out
}

// Count returns the number of discovered devices.
func (p *Pool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.devices)
}

// Rebalance assigns roles based on device count (any N).
func (p *Pool) Rebalance() RolePlan {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rebalanceLocked()
}

func (p *Pool) rebalanceLocked() RolePlan {
	n := len(p.devices)
	for i := range p.devices {
		p.devices[i].Role = RoleUnassigned
		p.devices[i].Assigned = false
	}
	plan := planRoles(n)
	idx := 0
	assign := func(role Role, count int) {
		for i := 0; i < count && idx < n; i++ {
			p.devices[idx].Role = role
			p.devices[idx].Assigned = true
			idx++
		}
	}
	assign(RoleControlHunt, plan.ControlHunt)
	assign(RoleVoice, plan.Voice)
	assign(RoleGMRS, plan.GMRS)
	assign(RoleHuntBackup, plan.HuntBackup)
	p.roles = plan
	return plan
}

// PlanRolesForN returns role distribution for n devices (exported for tests).
func PlanRolesForN(n int) RolePlan {
	return planRoles(n)
}

// planRoles distributes dongles across roles for any N >= 1.
func planRoles(n int) RolePlan {
	if n <= 0 {
		return RolePlan{}
	}
	if n == 1 {
		return RolePlan{ControlHunt: 1}
	}
	if n == 2 {
		return RolePlan{ControlHunt: 1, Voice: 1}
	}
	// N >= 3: control + voice + GMRS; extras go to voice then hunt backup.
	plan := RolePlan{ControlHunt: 1, Voice: 1, GMRS: 1}
	extra := n - 3
	for extra > 0 {
		plan.Voice++
		extra--
		if extra > 0 {
			plan.HuntBackup++
			extra--
		}
	}
	return plan
}

// ByRole returns devices assigned to a role.
func (p *Pool) ByRole(role Role) []Device {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []Device
	for _, d := range p.devices {
		if d.Role == role {
			out = append(out, d)
		}
	}
	return out
}

// RolePlan returns the current role distribution plan.
func (p *Pool) RolePlan() RolePlan {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.roles
}

// Attach handles hot-plug: re-enumerate and rebalance.
func (p *Pool) Attach() ([]Device, error) {
	return p.Discover()
}

// Detach handles hot-unplug by re-enumerating.
func (p *Pool) Detach() ([]Device, error) {
	return p.Discover()
}

// Summary returns a human-readable pool summary.
func (p *Pool) Summary() string {
	devices := p.Devices()
	plan := p.RolePlan()
	sort.Slice(devices, func(i, j int) bool { return devices[i].Index < devices[j].Index })
	s := fmt.Sprintf("%d device(s): control=%d voice=%d gmrs=%d hunt_backup=%d\n",
		len(devices), plan.ControlHunt, plan.Voice, plan.GMRS, plan.HuntBackup)
	for _, d := range devices {
		s += fmt.Sprintf("  [%d] %s serial=%s role=%s\n", d.Index, d.ID, d.Serial, d.Role)
	}
	return s
}
