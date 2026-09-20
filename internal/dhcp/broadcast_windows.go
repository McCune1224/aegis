//go:build windows

package dhcp

import "syscall"

// broadcastControl lets the socket send to 255.255.255.255, which is where a
// client that has no address yet can hear a reply.
func broadcastControl(_, _ string, raw syscall.RawConn) error {
	var failure error
	if err := raw.Control(func(fd uintptr) {
		failure = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return failure
}
