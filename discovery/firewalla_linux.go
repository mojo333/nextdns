//go:build linux
// +build linux

package discovery

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

//go:embed firewalla.lua
var firewallaRedisScript string

type Firewalla struct {
	OnError func(err error)

	once      sync.Once
	supported bool

	table macTable
}

func isFirewalla() bool {
	_, err := os.Stat("/etc/firewalla_release")
	return err == nil
}

func (r *Firewalla) init() {
	if isFirewalla() {
		r.supported = true
	}
}

func (r *Firewalla) refresh() {
	r.once.Do(r.init)
	if !r.supported {
		return
	}
	if err := r.table.refresh(5*time.Minute, r.clientList); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("clientList: %v", err))
	}
}

func (r *Firewalla) Name() string {
	return "firewalla"
}

func (r *Firewalla) Visit(f func(name string, macs []string)) {
	r.refresh()
	r.table.visit(f)
}

func (r *Firewalla) LookupMAC(mac string) []string {
	r.refresh()
	return r.table.lookupMAC(mac)
}

func (r *Firewalla) LookupAddr(addr string) []string {
	return nil
}

func (r *Firewalla) LookupHost(name string) []string {
	return nil
}

func (r *Firewalla) clientList() (map[string][]string, error) {
	lfh, err := os.CreateTemp("", "firewalla.lua")
	if err != nil {
		return nil, err
	}
	luaScript := lfh.Name()
	defer os.Remove(luaScript)
	_, werr := lfh.Write([]byte(firewallaRedisScript))
	if cerr := lfh.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return nil, werr
	}
	b, err := exec.Command("/usr/bin/redis-cli", "--eval", luaScript).Output()
	if err != nil {
		return nil, err
	}
	var macs map[string][]string
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&macs); err != nil {
		return nil, err
	}
	return macs, nil
}
