package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/maehld/paperboy/internal/paperboy"
)

func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("config", ".", "Instance directory with paperboy.yaml, pages, plans, providers and state")
	listen := flag.String("listen", ":8090", "HTTP listen address; must be reachable for the displays on Wi-Fi")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flag.Args())
	}
	cfg, err := paperboy.LoadConfig(*dir)
	if err != nil {
		slog.Error("paperboy.yaml not usable; starting degraded (it will be applied without a restart once it is valid)",
			"dir", *dir, "error", err)
		cfg = paperboy.DegradedConfig(*dir)
	}
	srv, err := paperboy.New(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	srv.StartProviders(ctx)
	server := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	slog.Info("Paperboy started", "listen", listener.Addr().String(), "display_types", len(cfg.Profiles), "devices", len(cfg.Devices))
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		return server.Shutdown(shutdownCtx)
	}
	return nil
}
