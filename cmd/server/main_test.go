package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func portOf(t *testing.T, serverURL string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestHealthcheckReportsTheServerState(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Errorf("probed %q, want /healthz", r.URL.Path)
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Setenv("PRUNEPLEX_PORT", portOf(t, healthy.URL))
	if got := healthcheck(); got != 0 {
		t.Fatalf("healthy server: healthcheck() = %d, want 0", got)
	}
	healthy.Close()

	// Once nothing is listening, the check must fail so the container is marked unhealthy.
	if got := healthcheck(); got != 1 {
		t.Fatalf("no server: healthcheck() = %d, want 1", got)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer broken.Close()
	t.Setenv("PRUNEPLEX_PORT", portOf(t, broken.URL))
	if got := healthcheck(); got != 1 {
		t.Fatalf("500 response: healthcheck() = %d, want 1", got)
	}
}

func TestListenAddressComesFromTheEnvironment(t *testing.T) {
	// Clear anything set in the environment running the tests.
	t.Setenv("PRUNEPLEX_PORT", "")
	t.Setenv("PORT", "")
	t.Setenv("PRUNEPLEX_BIND_ADDRESS", "")

	if got := listenAddress(); got != ":4848" {
		t.Fatalf("default = %q, want :4848 (all interfaces)", got)
	}
	t.Setenv("PORT", "5000")
	if got := listenAddress(); got != ":5000" {
		t.Fatalf("PORT fallback = %q, want :5000", got)
	}
	t.Setenv("PRUNEPLEX_PORT", "6000")
	if got := listenAddress(); got != ":6000" {
		t.Fatalf("PRUNEPLEX_PORT must win over PORT, got %q", got)
	}
	t.Setenv("PRUNEPLEX_BIND_ADDRESS", " 127.0.0.1 ")
	if got := listenAddress(); got != "127.0.0.1:6000" {
		t.Fatalf("bind address = %q, want 127.0.0.1:6000", got)
	}
	t.Setenv("PRUNEPLEX_BIND_ADDRESS", "::1")
	if got := listenAddress(); got != "[::1]:6000" {
		t.Fatalf("IPv6 bind address = %q, want [::1]:6000", got)
	}
}

func TestProbeHost(t *testing.T) {
	for address, want := range map[string]string{
		"":             "127.0.0.1", // every interface: use loopback
		"0.0.0.0":      "127.0.0.1",
		"::":           "127.0.0.1",
		"192.168.1.10": "192.168.1.10", // one specific address: that is the only place to reach it
		"myhost":       "myhost",
	} {
		t.Setenv("PRUNEPLEX_BIND_ADDRESS", address)
		if got := probeHost(); got != want {
			t.Errorf("probeHost() with bind address %q = %q, want %q", address, got, want)
		}
	}
}
