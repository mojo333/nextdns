package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

var exampleQuery = []byte{
	0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00,
	0x00, 0x01, 0x00, 0x01,
}

// fullInflight returns an in-flight channel with every slot already taken,
// as when slow upstream queries hold them all.
func fullInflight() chan struct{} {
	c := make(chan struct{}, 1)
	c <- struct{}{}
	return c
}

func TestServeUDP_AnswersServfailWhenNoSlotIsFree(t *testing.T) {
	// Arrange
	l, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var inflight sync.WaitGroup
	go func() { _ = (Proxy{}).serveUDP(ctx, l, fullInflight(), &inflight) }()

	client, err := net.Dial("udp4", l.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Act
	if _, err := client.Write(exampleQuery); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	reply := make([]byte, 512)
	n, err := client.Read(reply)

	// Assert: the query is refused at once rather than left waiting for a slot.
	if err != nil {
		t.Fatalf("no reply while every in-flight slot was taken: %v", err)
	}
	if n < 4 || binary.BigEndian.Uint16(reply) != 0x1234 || reply[3]&0x0f != 2 {
		t.Fatalf("reply = %x, want SERVFAIL for ID 1234", reply[:n])
	}
}

func TestServeTCPConn_RefusesWhenNoSlotIsFree(t *testing.T) {
	// Arrange
	server, client := net.Pipe()
	defer client.Close()
	errC := make(chan error, 1)
	go func() {
		errC <- (Proxy{}).serveTCPConn(context.Background(), server, fullInflight(), NewBufferPool())
	}()

	// Act: the connection is refused before the query is even read.
	go func() {
		frame := binary.BigEndian.AppendUint16(nil, uint16(len(exampleQuery)))
		_, _ = client.Write(append(frame, exampleQuery...))
	}()

	// Assert
	select {
	case err := <-errC:
		if !errors.Is(err, errTooManyInflightRequests) {
			t.Fatalf("serveTCPConn() err = %v, want errTooManyInflightRequests", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serveTCPConn blocked waiting for an in-flight slot")
	}
}
