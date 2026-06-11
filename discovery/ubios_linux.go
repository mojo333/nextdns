package discovery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Ubios struct {
	OnError func(err error)

	once      sync.Once
	supported bool

	table macTable
}

func isUnifi() bool {
	if st, _ := os.Stat("/data/unifi"); st != nil && st.IsDir() {
		return true
	}
	if err := exec.Command("ubnt-device-info", "firmware").Run(); err == nil {
		return true
	}
	return false
}

func (r *Ubios) init() {
	if isUnifi() {
		r.supported = true
	}
}

func (r *Ubios) refresh() {
	r.once.Do(r.init)
	if !r.supported {
		return
	}
	if err := r.table.refresh(5*time.Minute, clientListUbios); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("clientList: %v", err))
	}
}

func (r *Ubios) Name() string {
	return "ubios"
}

func (r *Ubios) Visit(f func(name string, macs []string)) {
	r.refresh()
	r.table.visit(f)
}

func (r *Ubios) LookupMAC(mac string) []string {
	r.refresh()
	return r.table.lookupMAC(mac)
}

func (r *Ubios) LookupAddr(addr string) []string {
	return nil
}

func (r *Ubios) LookupHost(name string) []string {
	return nil
}

func clientListUbios() (map[string][]string, error) {
	cmd := exec.Command("/usr/bin/mongo", "localhost:27117/ace", "--quiet", "--eval", `
		DBQuery.shellBatchSize = 1000;
		db.user.find({name: {$exists: true, $ne: ""}}, {_id:0, mac:1, name:1});`)
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	rec := struct {
		MAC  string
		Name string
	}{}
	macs := map[string][]string{}
	for d.Decode(&rec) == nil {
		mac := strings.ToLower(rec.MAC)
		macs[mac] = appendUniq(macs[mac], rec.Name)
	}
	return macs, nil
}
