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

	"solarai/internal/displayd"
)

func main() {
	if err := run(); err != nil {
		slog.Error("Start fehlgeschlagen", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "displayd.json", "Konfigurationsdatei (JSON)")
	dataDir := flag.String("data", "displayd-data", "Verzeichnis für Bilder und Gerätezustand")
	listen := flag.String("listen", ":8090", "HTTP-Listen-Adresse; muss für die Displays im WLAN erreichbar sein")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unerwartete Argumente: %v", flag.Args())
	}
	cfg, err := displayd.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		return err
	}
	srv, err := displayd.New(cfg, *dataDir)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	pushDone := make(chan struct{})
	go func() { defer close(pushDone); srv.Run(ctx) }()
	defer func() { cancel(); <-pushDone }()
	slog.Info("Display-Service gestartet", "listen", listener.Addr().String(), "profiles", len(cfg.Profiles), "devices", len(cfg.Devices))
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
