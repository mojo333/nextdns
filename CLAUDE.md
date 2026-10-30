# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

A personal fork of the NextDNS CLI ([nextdns/nextdns](https://github.com/nextdns/nextdns)), a DNS-over-HTTPS forwarding daemon aimed mainly at routers. It runs in production only on a UniFi Dream Machine Pro, which means the `linux_arm64` release binary and the UniFi OS paths (`router/ubios`, `discovery/ubios_linux.go` and `host/service/ubios`) are the ones that matter most. Fixes aimed at other routers are lower priority. The Go module path is still `github.com/nextdns/nextdns`, so imports look identical to upstream.

The fork is versioned `v2.x` and upstream is `v1.x`. It branched from upstream `master` at `4232ba7` (v1.46.0 plus eight unreleased commits), and upstream v1.47.1 and v1.47.2 were later ported by hand rather than merged, so no upstream release tag is an ancestor of `master`. The current plan is to retire the fork and return to upstream, carrying only the fixes upstream still lacks. Two consequences follow for day-to-day work:

- When upstream has already fixed something, take its fix and its tests unchanged and cite the upstream commit in the commit message, so the eventual diff against upstream stays clean.
- Upstream v1.48.0 removes `internal/dnsmessage` (a vendored copy of `golang.org/x/net/dns/dnsmessage`) in favour of `golang.org/x/net`. Anything ported from v1.48.0 onwards will need adapting while this fork still uses the internal copy.

## Commands

```sh
go build -o nextdns .                          # local binary; version reports "dev"
go test -race ./...                            # full suite
go test ./resolver/query/ -run TestName -v     # single test
go vet ./...
staticcheck $(go list ./... | grep -v internal/dnsmessage)   # as CI runs it
golangci-lint run ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...   # as CI runs it
```

Neither `staticcheck` nor `golangci-lint` is installed on this machine. CI runs both on every PR (`.github/workflows/staticcheck.yml` per GOOS for darwin and linux, and `golangci-lint.yml`). `govulncheck.yml` runs on every PR, on pushes to `master` and weekly. The `go` directive in `go.mod` is what pins the release toolchain, so a failing standard library finding is fixed by raising that directive to the patched Go release, not by changing code. Run govulncheck locally with `GOTOOLCHAIN` set to that exact version, because a newer local Go hides standard library findings.

Much of the code is split by platform using filename suffixes (`_linux.go`, `_bsd.go`, `_windows.go`, `_other.go`), so a change in `host/`, `router/`, `discovery/` or `ctl/` should be checked for the targets that actually ship:

```sh
GOOS=linux GOARCH=arm64 go vet ./...
GOOS=freebsd GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build -o /dev/null .
```

`GOOS=windows go vet ./...` already fails on `ctl/internal/winio/pipe_test.go`, which calls `t.Fatal` from goroutines and is inherited from upstream, so a Windows build is the meaningful check there.

## Releases and binaries

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which cross-compiles linux amd64/arm64/armv7 and freebsd amd64/arm64 with `CGO_ENABLED=0` and `-ldflags "-s -w -X main.version=<tag without v>"`, then attaches tarballs and `SHA256SUMS` to the GitHub release. Those release assets are the production artefacts.

Upstream's GoReleaser packaging (deb, rpm, apk and Homebrew) is not used in this fork and has been removed; it returns with the move back to upstream. Binaries are not committed to the repository; the release assets and their `SHA256SUMS` are the only artefacts. Check a downloaded binary with `go version <binary>` and `strings <binary> | grep -x '2\.[0-9]*\.[0-9]*'`. `nextdns upgrade` deliberately refuses to run, because upstream's installer would replace the fork with upstream's release; upgrades use the fork's releases instead, and `upgrade.go` says how to restore it. `install-custom.sh` installs from a fork's releases via `CUSTOM_GITHUB_REPO`, and it defaults to upstream when that variable is unset.

## Architecture

The program is a single binary whose subcommands are dispatched from the table in `main.go`. `run` is the daemon, and most other subcommands either manage the OS service (`host/service`) or query the running daemon over the control socket (`ctl`).

`run.go` wires everything together, and it is the best place to start reading. A query flows through the following stages:

1. **`proxy`** listens on UDP and TCP, bounds in-flight work and builds a `query.Query` for each request.
2. **`resolver/query`** parses the wire message lazily. It extracts EDNS0 client subnet (which becomes `PeerIP`) and the dnsmasq `--add-mac` option (`EDNS0_MAC`), and it uses the `arp` and `ndp` tables to map between client IP and MAC. This parser handles untrusted input from the LAN, so a bug here is a denial-of-service or filter-bypass risk rather than a cosmetic one.
3. **`config.Forwarders`** chooses an upstream by domain, applying conditional forwarders first, and the last entry is always the NextDNS resolver.
4. **`resolver.DNS`** dispatches to `DOH` or `DNS53`, both backed by one shared byte-limited Ristretto cache (`resolver/bytecache.go`). The DoH profile is chosen per query by `GetProfileURL`, which evaluates conditional profiles in `config/profile.go` that match on subnet, interface or MAC.
5. **`resolver/endpoint.Manager`** keeps a set of DoH endpoints, tests them, fails over between providers and recovers. Its endpoint tests run alongside live traffic, so locking in it affects query latency.

Alongside that path:

- **`discovery`** names LAN clients for NextDNS analytics. It combines several sources (hosts file, mDNS, DHCP lease files, reverse DNS and router-specific sources for Merlin, UniFi OS and Firewalla) into one `discovery.Resolver`. The `discovered` subcommand exposes the result. mDNS and DNS discovery accept unsolicited LAN traffic, so their caches must stay bounded.
- **`router`** detects the platform it runs on (OpenWrt, UniFi OS, Firewalla, Merlin, EdgeOS, DD-WRT, Synology or generic) and implements `Configure` (adjust config before listening), `Setup` (hook into the router's dnsmasq or equivalent after listening) and `Restore` (undo it on stop).
- **`host`** covers OS integration: system resolver activation and deactivation, logging back ends and model detection. `host/service` contains one implementation per init system (systemd, procd, openrc, launchd, Windows and others) behind a common `Service` interface.
- **`ctl`** is the control channel between the CLI and the daemon. It uses a Unix socket on Unix-like systems and a named pipe on Windows (`ctl/internal/winio`), and the daemon registers handlers with `ctl.Command`.
