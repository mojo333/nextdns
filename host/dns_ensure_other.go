//go:build !linux
// +build !linux

package host

func EnsureDNS(dns string, port uint16) (bool, error) {
	_ = dns
	_ = port
	return false, nil
}
