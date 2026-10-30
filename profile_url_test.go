package main

import (
	"net"
	"testing"

	"github.com/nextdns/nextdns/config"
	"github.com/nextdns/nextdns/resolver/query"
)

func TestProfileURLFunc_SingleProfileReportsProfile(t *testing.T) {
	// Arrange
	var profiles config.Profiles
	if err := profiles.Set("abc123"); err != nil {
		t.Fatal(err)
	}

	// Act
	url, profile := profileURLFunc(profiles)(query.Query{})

	// Assert: the query log shows this profile, so it must not be empty.
	if url != "https://dns.nextdns.io/abc123" || profile != "abc123" {
		t.Fatalf("got (%q, %q), want (https://dns.nextdns.io/abc123, abc123)", url, profile)
	}
}

func TestProfileURLFunc_ConditionalProfileBySourceSubnet(t *testing.T) {
	// Arrange
	var profiles config.Profiles
	for _, v := range []string{"10.0.0.0/24=kids01", "default01"} {
		if err := profiles.Set(v); err != nil {
			t.Fatal(err)
		}
	}
	f := profileURLFunc(profiles)

	// Act and Assert
	if _, profile := f(query.Query{PeerIP: net.ParseIP("10.0.0.5")}); profile != "kids01" {
		t.Fatalf("profile for 10.0.0.5 = %q, want kids01", profile)
	}
	if _, profile := f(query.Query{PeerIP: net.ParseIP("192.0.2.5")}); profile != "default01" {
		t.Fatalf("profile for 192.0.2.5 = %q, want default01", profile)
	}
}
