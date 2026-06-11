package discovery

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMACTableRefreshPopulatesLookups(t *testing.T) {
	// Arrange
	var tbl macTable
	update := func() (map[string][]string, error) {
		return map[string][]string{
			"aa:bb:cc:dd:ee:ff": {"laptop", "laptop-wifi"},
			"11:22:33:44:55:66": {"phone"},
		}, nil
	}

	// Act
	if err := tbl.refresh(time.Minute, update); err != nil {
		t.Fatalf("refresh: unexpected error: %v", err)
	}

	// Assert
	if got, want := tbl.lookupMAC("aa:bb:cc:dd:ee:ff"), []string{"laptop", "laptop-wifi"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lookupMAC = %v, want %v", got, want)
	}
	if got := tbl.lookupMAC("00:00:00:00:00:00"); got != nil {
		t.Errorf("lookupMAC for unknown MAC = %v, want nil", got)
	}
	visited := map[string][]string{}
	tbl.visit(func(name string, macs []string) {
		visited[name] = macs
	})
	if got, want := visited["phone"], []string{"11:22:33:44:55:66"}; !reflect.DeepEqual(got, want) {
		t.Errorf("visit phone = %v, want %v", got, want)
	}
	if len(visited) != 3 {
		t.Errorf("visit returned %d names, want 3", len(visited))
	}
}

func TestMACTableRefreshHonorsTTL(t *testing.T) {
	// Arrange
	var tbl macTable
	calls := 0
	update := func() (map[string][]string, error) {
		calls++
		return map[string][]string{"aa:bb": {"host"}}, nil
	}

	// Act: second refresh within the TTL must not call update again.
	_ = tbl.refresh(time.Hour, update)
	_ = tbl.refresh(time.Hour, update)

	// Assert
	if calls != 1 {
		t.Errorf("update called %d times within TTL, want 1", calls)
	}
}

func TestMACTableRefreshErrorKeepsExistingEntries(t *testing.T) {
	// Arrange
	var tbl macTable
	_ = tbl.refresh(0, func() (map[string][]string, error) {
		return map[string][]string{"aa:bb": {"host"}}, nil
	})

	// Act
	err := tbl.refresh(0, func() (map[string][]string, error) {
		return nil, errors.New("boom")
	})

	// Assert
	if err == nil {
		t.Fatal("refresh did not propagate update error")
	}
	if got, want := tbl.lookupMAC("aa:bb"), []string{"host"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lookupMAC after failed refresh = %v, want %v (stale data preserved)", got, want)
	}
}

func TestMACTableRefreshErrorStillRateLimits(t *testing.T) {
	// Arrange
	var tbl macTable
	calls := 0
	update := func() (map[string][]string, error) {
		calls++
		return nil, errors.New("boom")
	}

	// Act: a failed refresh must still set the expiry so a tight query loop
	// does not hammer the underlying command.
	_ = tbl.refresh(time.Hour, update)
	_ = tbl.refresh(time.Hour, update)

	// Assert
	if calls != 1 {
		t.Errorf("update called %d times within TTL after error, want 1", calls)
	}
}

// TestMACTableConcurrentAccess exercises concurrent refreshes, lookups and
// visits. A zero TTL forces every refresh to re-run update, so the race
// detector sees constant writers alongside the readers. Run with -race.
func TestMACTableConcurrentAccess(t *testing.T) {
	// Arrange
	var tbl macTable
	update := func() (map[string][]string, error) {
		return map[string][]string{
			"aa:bb:cc:dd:ee:ff": {"laptop"},
			"11:22:33:44:55:66": {"phone"},
		}, nil
	}

	// Act
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_ = tbl.refresh(0, update)
				_ = tbl.lookupMAC("aa:bb:cc:dd:ee:ff")
				tbl.visit(func(string, []string) {})
			}
		}()
	}
	wg.Wait()

	// Assert
	if got := tbl.lookupMAC("11:22:33:44:55:66"); len(got) != 1 {
		t.Errorf("lookupMAC after concurrent refreshes = %v, want 1 name", got)
	}
}
