package resolver

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/nextdns/nextdns/internal/testutil"
	"github.com/nextdns/nextdns/resolver/query"
)

func Test_cacheValue_AdjustedResponse(t *testing.T) {
	type fields struct {
		time time.Time
		msg  []byte
	}
	now := time.Now()
	tests := []struct {
		name       string
		fields     fields
		id         uint16
		wantB      []byte
		wantMinTTL uint32
	}{
		{
			"Empty Record",
			fields{
				now.Add(-10 * time.Second),
				[]byte{},
			},
			123,
			[]byte{},
			0,
		},
		{
			"Happy Path",
			fields{
				now.Add(-10 * time.Second),
				[]byte{
					0xa6, 0xed, // ID
					0x81, 0x80, // Flags
					0x00, 0x01, // Questions
					0x00, 0x01, // Answers
					0x00, 0x00, // Authorities
					0x00, 0x01, // Additionals
					// Questions
					0x04, 0x74, 0x65, 0x73, 0x74, 0x03, 0x63, 0x6f, 0x6d, 0x00, // Label test.com.
					0x00, 0x01, // Type A
					0x00, 0x01, // Class IN
					// Answers
					0xc0, 0x0c, // Label pointer test.com.
					0x00, 0x01, // Type A
					0x00, 0x01, // Class IN
					0x00, 0x00, 0x0e, 0x10, // TTL 3600
					0x00, 0x04, // Data len 4
					0x45, 0xac, 0xc8, 0xeb, // 69.172.200.
					// Additionals
					0x00,       // Label <root>
					0x00, 0x29, // Type OPT
					0x05, 0xac, // UDP packet size
					0x00,       // Extended RCODE
					0x00,       // EDNS Version
					0x00, 0x00, // Flags
					0x00, 0x00, // DATA
				},
			},
			123,
			[]byte{
				0x00, 0x7b, // ID = 123
				0x81, 0x80, // Flags
				0x00, 0x01, // Questions
				0x00, 0x01, // Answers
				0x00, 0x00, // Authorities
				0x00, 0x01, // Additionals
				// Questions
				0x04, 0x74, 0x65, 0x73, 0x74, 0x03, 0x63, 0x6f, 0x6d, 0x00, // Label test.com.
				0x00, 0x01, // Type A
				0x00, 0x01, // Class IN
				// Answers
				0xc0, 0x0c, // Label pointer test.com.
				0x00, 0x01, // Type A
				0x00, 0x01, // Class IN
				0x00, 0x00, 0x0e, 0x06, // TTL 3600 - 10
				0x00, 0x04, // Data len 4
				0x45, 0xac, 0xc8, 0xeb, // 69.172.200.235
				// Additionals
				0x00,       // Label <root>
				0x00, 0x29, // Type OPT
				0x05, 0xac, // UDP packet size
				0x00,       // Extended RCODE
				0x00,       // EDNS Version
				0x00, 0x00, // Flags
				0x00, 0x00, // DATA
			},
			3600 - 10,
		},
		// TODO: fuzz
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := cacheValue{
				time: tt.fields.time,
				msg:  tt.fields.msg,
			}
			buf := make([]byte, 4096)
			n, gotMinTTL := v.AdjustedResponse(buf, tt.id, 0, 0, now)
			if gotB := buf[:n]; !reflect.DeepEqual(gotB, tt.wantB) {
				t.Errorf("cacheValue.AdjustedResponse()\ngotB:\n%#v\nwant:\n%#v", gotB, tt.wantB)
			}
			if gotMinTTL != tt.wantMinTTL {
				t.Errorf("cacheValue.AdjustedResponse() gotMinTTL = %v, want %v", gotMinTTL, tt.wantMinTTL)
			}
		})
	}
}

func Test_cacheKey_Hash_Deterministic(t *testing.T) {
	k := cacheKey{ctx: "https://dns.nextdns.io/abc123", qclass: query.ClassINET, qtype: query.TypeA, qname: "example.com."}
	h1 := k.Hash()
	h2 := k.Hash()
	if h1 != h2 {
		t.Errorf("Hash not deterministic: %d != %d", h1, h2)
	}
}

func Test_cacheKey_Hash_DifferentKeys(t *testing.T) {
	keys := []cacheKey{
		{ctx: "", qclass: query.ClassINET, qtype: query.TypeA, qname: "example.com."},
		{ctx: "", qclass: query.ClassINET, qtype: query.TypeAAAA, qname: "example.com."},
		{ctx: "", qclass: query.ClassINET, qtype: query.TypeA, qname: "other.com."},
		{ctx: "https://dns.nextdns.io", qclass: query.ClassINET, qtype: query.TypeA, qname: "example.com."},
		{ctx: "", qclass: query.ClassCHAOS, qtype: query.TypeA, qname: "example.com."},
	}
	seen := map[uint64]int{}
	for i, k := range keys {
		h := k.Hash()
		if prev, ok := seen[h]; ok {
			t.Errorf("keys[%d] and keys[%d] have same hash %d", prev, i, h)
		}
		seen[h] = i
	}
}

func Test_cacheKey_ValidateQuestion(t *testing.T) {
	// Build a valid DNS response for "example.com." A IN
	resp, err := testutil.NewTestResponse(1234, "example.com.", net.ParseIP("1.2.3.4"), 300)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		key  cacheKey
		msg  []byte
		want bool
	}{
		{
			name: "matching key",
			key:  cacheKey{qclass: query.ClassINET, qtype: query.TypeA, qname: "example.com."},
			msg:  resp,
			want: true,
		},
		{
			name: "wrong qtype",
			key:  cacheKey{qclass: query.ClassINET, qtype: query.TypeAAAA, qname: "example.com."},
			msg:  resp,
			want: false,
		},
		{
			name: "wrong qname",
			key:  cacheKey{qclass: query.ClassINET, qtype: query.TypeA, qname: "other.com."},
			msg:  resp,
			want: false,
		},
		{
			name: "wrong qclass",
			key:  cacheKey{qclass: query.ClassCHAOS, qtype: query.TypeA, qname: "example.com."},
			msg:  resp,
			want: false,
		},
		{
			name: "empty message",
			key:  cacheKey{qclass: query.ClassINET, qtype: query.TypeA, qname: "example.com."},
			msg:  []byte{},
			want: false,
		},
		{
			name: "truncated message",
			key:  cacheKey{qclass: query.ClassINET, qtype: query.TypeA, qname: "example.com."},
			msg:  resp[:6],
			want: false,
		},
		{
			name: "nil message",
			key:  cacheKey{qclass: query.ClassINET, qtype: query.TypeA, qname: "example.com."},
			msg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.key.ValidateQuestion(tt.msg)
			if got != tt.want {
				t.Errorf("ValidateQuestion() = %v, want %v", got, tt.want)
			}
		})
	}
}
