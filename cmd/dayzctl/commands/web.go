package commands

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/logger"
	"github.com/dayzctl/dayzctl/internal/webapp"
)

// DefaultWebPort is used when neither the flag nor the config sets a port.
const DefaultWebPort = 8081

// WebOptions describes how the web UI should listen. Zero values fall back to
// the configuration file, then to built-in defaults.
type WebOptions struct {
	Address string
	Port    int
}

// WebAction runs the web UI and its JSON API in the foreground until the
// process is asked to stop.
//
// It reads the profiles folder of every configured instance and serves the
// embedded single-page app; nothing is ever written to the DayZ install.
func WebAction(opts WebOptions) error {
	if shared.Config == nil {
		return errors.New("config not loaded")
	}

	address := firstNonEmpty(opts.Address, shared.Config.Web.Address, "127.0.0.1")
	port := opts.Port
	if port == 0 {
		port = shared.Config.Web.Port
	}
	if port == 0 {
		port = DefaultWebPort
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d", port)
	}

	server, err := webapp.New(shared.Config)
	if err != nil {
		return fmt.Errorf("failed to create web server: %w", err)
	}

	listenAddr := net.JoinHostPort(address, strconv.Itoa(port))
	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Analyses of large logs can take a while to stream out; do not cut
		// the response short with a write deadline.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	logger.Info("Web UI listening", "address", listenAddr, "installDir", shared.Config.GetInstallDir())
	fmt.Printf("dayzctl web listening on http://%s\n", listenAddr)
	fmt.Println("Press Ctrl+C to stop.")

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("web server failed: %w", err)
		}
	case <-ctx.Done():
		fmt.Println("\nShutting down…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown failed: %w", err)
		}
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
