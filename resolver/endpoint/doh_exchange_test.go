package endpoint

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"testing/iotest"
)

func TestDOHEndpoint_ExchangeReadsBodyDeliveredInPieces(t *testing.T) {
	want := []byte("a reply that arrives one byte at a time")
	e := &DOHEndpoint{transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		body := iotest.OneByteReader(bytes.NewReader(want))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body)}, nil
	})}
	buf := make([]byte, 514)

	n, err := e.Exchange(context.Background(), []byte("query"), buf)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got := buf[:n]; !bytes.Equal(got, want) {
		t.Fatalf("Exchange read %q, want %q", got, want)
	}
}
