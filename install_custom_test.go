//go:build !windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const releaseAsset = "nextdns_9.9.9_linux_arm64.tar.gz"

func releaseArchive(t *testing.T, content string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "nextdns", Mode: 0755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// installBinary runs install-custom.sh's install_bin for release, with curl
// serving files in place of the network, over an existing binary. It returns
// whether it succeeded and what the installed binary then contains.
func installBinary(t *testing.T, release string, files map[string][]byte) (bool, string) {
	t.Helper()
	script, err := os.ReadFile("install-custom.sh")
	if err != nil {
		t.Fatal(err)
	}
	functions, found := strings.CutSuffix(strings.TrimRight(string(script), "\n"), "\nmain")
	if !found {
		t.Fatal("install-custom.sh no longer ends by calling main")
	}
	server := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(server, name), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(t.TempDir(), "nextdns")
	if err := os.WriteFile(bin, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	harness := functions + `
asroot() { "$@"; }
curl() {
	out=; url=
	while [ $# -gt 0 ]; do
		case $1 in
		-o) out=$2; shift ;;
		http*) url=$1 ;;
		esac
		shift
	done
	file="$SERVER/${url##*/}"
	[ -f "$file" ] || return 22
	if [ "$out" ]; then cp "$file" "$out"; else cat "$file"; fi
}
GOOS=linux GOARCH=arm64 INSTALL_RELEASE="$RELEASE" NEXTDNS_BIN="$BIN"
install_bin
`
	c := exec.Command("sh", "-c", harness)
	c.Env = append(os.Environ(), "SERVER="+server, "BIN="+bin, "RELEASE="+release,
		"CUSTOM_GITHUB_REPO=example/nextdns")
	out, runErr := c.CombinedOutput()
	t.Logf("installer output:\n%s", out)
	installed, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	return runErr == nil, string(installed)
}

func TestInstallCustom_VerifiesTheDownload(t *testing.T) {
	archive := releaseArchive(t, "new")
	sums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), releaseAsset))
	tampered := releaseArchive(t, "evil")
	tests := []struct {
		name    string
		release string
		files   map[string][]byte
		ok      bool
	}{
		{"matching SHA256SUMS", "9.9.9", map[string][]byte{releaseAsset: archive, "SHA256SUMS": sums}, true},
		{"matching checksums.txt, as upstream names it", "9.9.9", map[string][]byte{releaseAsset: archive, "checksums.txt": sums}, true},
		{"archive differs from its checksum", "9.9.9", map[string][]byte{releaseAsset: tampered, "SHA256SUMS": sums}, false},
		{"release without checksums", "9.9.9", map[string][]byte{releaseAsset: archive}, false},
		{"archive missing from the checksums", "9.9.9", map[string][]byte{releaseAsset: archive, "SHA256SUMS": []byte("0000  other.tar.gz\n")}, false},
		{"archive not found", "9.9.9", map[string][]byte{"SHA256SUMS": sums}, false},
		{"snapshot build, which has no checksums", "master/abc123", map[string][]byte{"nextdns_linux_arm64.tar.gz": archive}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, installed := installBinary(t, tt.release, tt.files)

			want := "old"
			if tt.ok {
				want = "new"
			}
			if ok != tt.ok || installed != want {
				t.Fatalf("succeeded = %v with binary %q, want %v with %q", ok, installed, tt.ok, want)
			}
		})
	}
}

// The old path built a shell command from the release name, then removed the
// installed service before finding out whether a snapshot could be fetched.
func TestInstallCustom_RefusesUniFiSnapshots(t *testing.T) {
	script, err := os.ReadFile("install-custom.sh")
	if err != nil {
		t.Fatal(err)
	}
	functions := strings.TrimSuffix(strings.TrimRight(string(script), "\n"), "\nmain")
	for _, fn := range []string{"install_ubios_snapshot", "upgrade_ubios_snapshot"} {
		ran := filepath.Join(t.TempDir(), "ran")
		c := exec.Command("sh", "-c", functions+`
podman() { touch "$RAN"; }
INSTALL_RELEASE=master/abc123
`+fn)
		c.Env = append(os.Environ(), "RAN="+ran)
		if out, err := c.CombinedOutput(); err == nil {
			t.Errorf("%s succeeded: %s", fn, out)
		}
		if _, err := os.Stat(ran); err == nil {
			t.Errorf("%s ran podman", fn)
		}
	}
}
