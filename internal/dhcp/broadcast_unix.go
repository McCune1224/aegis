//go:build unix

package dhcp

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// broadcastControl lets the socket send to 255.255.255.255, which is where a
// client that has no address yet can hear a reply.
func broadcastControl(_, _ string, raw syscall.RawConn) error {
	var failure error
	if err := raw.Control(func(fd uintptr) {
		failure = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return failure
}
