package query

import (
	"net"
	"testing"
)

// queryWithECSOption builds an A query for "a." whose OPT record carries an
// EDNS Client Subnet option naming the IPv4 host 10.0.10.99/32.
func queryWithECSOption() []byte {
	return []byte{
		0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, // header qd=1 ar=1
		0x01, 'a', 0x00, 0x00, 0x01, 0x00, 0x01, // question a. A IN
		0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, // OPT, class 4096, ttl 0
		0x00, 0x0c, // rdlength
		0x00, 0x08, 0x00, 0x08, // ECS, length 8
		0x00, 0x01, 0x20, 0x00, 10, 0, 10, 99, // IPv4, /32
	}
}

func TestNew_IgnoresClientSubnetFromLANClient(t *testing.T) {
	// A LAN client must not choose a per-subnet profile by naming another
	// device's address.
	peer := net.ParseIP("192.0.2.50")
	payload := queryWithECSOption()

	q, err := New(payload, peer, net.ParseIP("192.0.2.1"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !q.PeerIP.Equal(peer) {
		t.Fatalf("PeerIP = %v, want the sender %v", q.PeerIP, peer)
	}
	// It must still be removed before the query goes upstream.
	if got := payload[len(payload)-4:]; !net.IP(got).Equal(net.IPv4zero) {
		t.Fatalf("client subnet %v left in the upstream query", net.IP(got))
	}
}

func TestNew_UsesClientSubnetFromLoopbackForwarder(t *testing.T) {
	q, err := New(queryWithECSOption(), net.IPv4(127, 0, 0, 1), net.IPv4(127, 0, 0, 1))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if want := net.IPv4(10, 0, 10, 99); !q.PeerIP.Equal(want) {
		t.Fatalf("PeerIP = %v, want %v from the loopback forwarder", q.PeerIP, want)
	}
}
