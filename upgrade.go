package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	installerURL = "https://nextdns.io/install"

	// installerFetchTimeout bounds the download of the installer script.
	installerFetchTimeout = 1 * time.Minute

	// maxInstallerScriptSize rejects bodies that cannot be a legitimate
	// installer script (the real one is a few tens of KB).
	maxInstallerScriptSize = 1 << 20 // 1 MiB
)

func upgrade(args []string) error {
	return installer("upgrade")
}

func installer(cmd string) error {
	ctx, cancel := context.WithTimeout(context.Background(), installerFetchTimeout)
	defer cancel()
	script, err := fetchInstallerScript(ctx, installerURL)
	if err != nil {
		return err
	}
	c := exec.Command("sh", "-c", script)
	c.Env = append(os.Environ(), "RUN_COMMAND="+cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// fetchInstallerScript downloads the installer script, refusing unexpected
// statuses and oversized bodies. Transport security relies on TLS to the
// installer host; the script is run with elevated privileges, so fail closed
// on anything anomalous.
func fetchInstallerScript(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("fetch installer: %w", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch installer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch installer: unexpected status: %s", res.Status)
	}
	var script strings.Builder
	if _, err := io.Copy(&script, io.LimitReader(res.Body, maxInstallerScriptSize+1)); err != nil {
		return "", fmt.Errorf("fetch installer: read body: %w", err)
	}
	if script.Len() > maxInstallerScriptSize {
		return "", fmt.Errorf("fetch installer: script larger than %d bytes", maxInstallerScriptSize)
	}
	return script.String(), nil
}
