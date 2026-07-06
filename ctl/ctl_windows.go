package ctl

import (
	"net"

	"github.com/nextdns/nextdns/ctl/internal/winio"
)

func listen(addr string) (net.Listener, error) {
	// Restrict the control pipe to SYSTEM and the local Administrators group.
	// Granting Everyone (WD) let any local user read goroutine dumps and the
	// full LAN device inventory from this privileged daemon.
	return winio.ListenPipe(`\\.\pipe\`+addr, &winio.PipeConfig{
		SecurityDescriptor: "O:SYD:P(A;;GA;;;SY)(A;;GA;;;BA)",
	})
}

func dial(addr string) (net.Conn, error) {
	return winio.DialPipe(`\\.\pipe\`+addr, nil)
}
