package query

import (
	"bytes"
	"net"
	"testing"
)

// queryWithMACOption builds an A query for "a." whose OPT record carries the
// dnsmasq --add-mac option (EDNS0_MAC) with the given value.
func queryWithMACOption(mac []byte) []byte {
	msg := []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // header qd=1 ar=1
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01, // question a. A IN
		0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, // OPT, class 4096, ttl 0
		0x00, byte(4 + len(mac)), // rdlength
		byte(EDNS0_MAC >> 8), byte(EDNS0_MAC & 0xff), 0x00, byte(len(mac)),
	}
	return append(msg, mac...)
}

var spoofedMAC = []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}

func TestNew_IgnoresMACOptionFromLANClient(t *testing.T) {
	// A LAN client must not choose a per-MAC profile by naming another
	// device's MAC; only dnsmasq on loopback adds this option legitimately.
	q, err := New(queryWithMACOption(spoofedMAC), net.ParseIP("192.0.2.50"), net.ParseIP("192.0.2.1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if bytes.Equal(q.MAC, spoofedMAC) {
		t.Fatalf("MAC = %v, taken from a LAN client's EDNS0 option", q.MAC)
	}
}

func TestNew_UsesMACOptionFromLoopbackForwarder(t *testing.T) {
	q, err := New(queryWithMACOption(spoofedMAC), net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !bytes.Equal(q.MAC, spoofedMAC) {
		t.Fatalf("MAC = %v, want %v from the loopback forwarder", q.MAC, net.HardwareAddr(spoofedMAC))
	}
}

func TestNew_IgnoresMalformedMACOption(t *testing.T) {
	q, err := New(queryWithMACOption([]byte{0xde, 0xad, 0xbe}), net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if q.MAC != nil {
		t.Fatalf("MAC = %v, want nil for a 3-byte option", q.MAC)
	}
}
