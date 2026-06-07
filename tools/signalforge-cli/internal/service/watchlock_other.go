//go:build !unix && !windows

package service

func processAlive(pid int) bool {
	return pid > 0
}
