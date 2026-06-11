package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchInstallerScriptReturnsBody(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\necho ok\n"))
	}))
	defer srv.Close()

	// Act
	script, err := fetchInstallerScript(context.Background(), srv.URL)

	// Assert
	if err != nil {
		t.Fatalf("fetchInstallerScript: unexpected error: %v", err)
	}
	if script != "#!/bin/sh\necho ok\n" {
		t.Errorf("script = %q, want shell script body", script)
	}
}

func TestFetchInstallerScriptRejectsNonOKStatus(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	// Act
	_, err := fetchInstallerScript(context.Background(), srv.URL)

	// Assert
	if err == nil {
		t.Fatal("fetchInstallerScript accepted a non-200 response")
	}
}

func TestFetchInstallerScriptRejectsOversizedBody(t *testing.T) {
	// Arrange
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxInstallerScriptSize+1)))
	}))
	defer srv.Close()

	// Act
	_, err := fetchInstallerScript(context.Background(), srv.URL)

	// Assert
	if err == nil {
		t.Fatal("fetchInstallerScript accepted a body larger than the size limit")
	}
}

func TestFetchInstallerScriptHonorsContextCancellation(t *testing.T) {
	// Arrange
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer srv.Close()
	defer close(blocked)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Act
	_, err := fetchInstallerScript(ctx, srv.URL)

	// Assert
	if err == nil {
		t.Fatal("fetchInstallerScript did not honor context cancellation")
	}
}
