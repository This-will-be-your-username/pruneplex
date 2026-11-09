package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pruneplex/internal/web"
)

// defaultPort is where the server listens unless PRUNEPLEX_PORT (or PORT) says otherwise.
const defaultPort = "4848"

func main() {
	// `pruneplex healthcheck` lets a container health check probe the server
	// without a shell or curl in the image.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	server := &http.Server{
		Addr:         listenAddress(),
		Handler:      web.NewHandler(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server listening", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown failed", "error", err)
			os.Exit(1)
		}
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// healthcheck reports 0 if the local server answers /healthz, else 1.
func healthcheck() int {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort(probeHost(), port()) + "/healthz")
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// port is the port to listen on: PRUNEPLEX_PORT, else PORT, else the default.
func port() string {
	if value := os.Getenv("PRUNEPLEX_PORT"); value != "" {
		return value
	}
	return envOr("PORT", defaultPort)
}

// bindAddress is the address to listen on: PRUNEPLEX_BIND_ADDRESS, or empty for
// every interface.
func bindAddress() string {
	return strings.TrimSpace(os.Getenv("PRUNEPLEX_BIND_ADDRESS"))
}

// listenAddress is the host:port the server listens on.
func listenAddress() string {
	return net.JoinHostPort(bindAddress(), port())
}

// probeHost is where the health check connects: the bind address when one
// specific address is set, otherwise the loopback address.
func probeHost() string {
	if ip := net.ParseIP(bindAddress()); ip != nil && !ip.IsUnspecified() {
		return ip.String()
	}
	if host := bindAddress(); host != "" && net.ParseIP(host) == nil {
		return host // a hostname
	}
	return "127.0.0.1"
}
