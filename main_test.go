package main

import "testing"

// run registers "cache-metrics" on the control socket, so the CLI must offer it.
func TestCommands_OfferCacheMetrics(t *testing.T) {
	for _, c := range commands {
		if c.name == "cache-metrics" {
			return
		}
	}
	t.Error("cache-metrics is not offered")
}
