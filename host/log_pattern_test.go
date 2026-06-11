package host

import "testing"

func TestLogGrepPattern(t *testing.T) {
	tests := []struct {
		name    string
		service string
		want    string
		wantErr bool
	}{
		{
			name:    "plain service name",
			service: "nextdns",
			want:    ` nextdns\(:\|\[\)`,
		},
		{
			name:    "rejects regex metacharacters",
			service: `next.dns\|evil`,
			wantErr: true,
		},
		{
			name:    "rejects spaces",
			service: "next dns",
			wantErr: true,
		},
		{
			name:    "rejects empty name",
			service: "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := logGrepPattern(tt.service)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("logGrepPattern(%q) accepted an unsafe name", tt.service)
				}
				return
			}
			if err != nil {
				t.Fatalf("logGrepPattern(%q): unexpected error: %v", tt.service, err)
			}
			if got != tt.want {
				t.Errorf("logGrepPattern(%q) = %q, want %q", tt.service, got, tt.want)
			}
		})
	}
}
