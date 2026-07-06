//go:build !windows
// +build !windows

package ctl

import (
	"net"
	"os"
)

func listen(addr string) (net.Listener, error) {
	_ = os.Remove(addr)
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: addr, Net: "unix"})
	if l != nil {
		l.SetUnlinkOnClose(true)
		// Don't rely on the process umask: restrict the control socket to the
		// owning (root) user so unprivileged local users cannot issue commands.
		if cerr := os.Chmod(addr, 0600); cerr != nil {
			_ = l.Close()
			return nil, cerr
		}
	}
	return l, err
}

func dial(addr string) (net.Conn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: addr, Net: "unix"})
}
