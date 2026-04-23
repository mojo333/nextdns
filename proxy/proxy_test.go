package proxy

import "testing"

func TestProxy_maxInflightRequests(t *testing.T) {
	tests := []struct {
		name string
		p    Proxy
		want int
	}{
		{
			name: "default when unset",
			p:    Proxy{},
			want: defaultMaxInflightRequests,
		},
		{
			name: "configured limit",
			p:    Proxy{MaxInflightRequests: 123},
			want: 123,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.maxInflightRequests(); got != tt.want {
				t.Fatalf("maxInflightRequests() = %d, want %d", got, tt.want)
			}
		})
	}
}
