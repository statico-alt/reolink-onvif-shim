// Command reolink-onvif-shim is a tiny ONVIF control-plane server that makes
// a Reolink camera adoptable by UniFi Protect as a third-party ONVIF camera.
// It answers just enough ONVIF SOAP for Protect to adopt the camera. In
// "proxy" mode it also relays the RTSP stream (a raw TCP byte proxy) and JPEG
// snapshots from the camera, because Protect fetches media from this host's
// IP rather than the address in the ONVIF URIs. In "direct" mode it instead
// hands Protect the camera's own URLs and no media passes through it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/statico-alt/reolink-onvif-shim/internal/config"
	"github.com/statico-alt/reolink-onvif-shim/internal/onvif"
)

func main() {
	configPath := flag.String("config", "./config.json", "path to config.json")
	logPath := flag.String("log", "./reolink-onvif-shim.log", "path to log file (also logs to stderr)")
	memInterval := flag.Duration("memstats-interval", time.Minute, "how often to log memory stats (0 to disable)")
	flag.Parse()

	logger, closeLog, err := setupLogging(*logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reolink-onvif-shim: %v\n", err)
		os.Exit(1)
	}
	defer closeLog()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatalf("loading config: %v", err)
	}

	logStartupConfig(logger, cfg, *configPath, *logPath)
	startMemStatsLogger(logger, *memInterval)

	svc := onvif.NewService(cfg, logger)
	handler := onvif.NewServer(svc, cfg, logger)

	mux := http.NewServeMux()
	mux.Handle("/onvif/device_service", handler)
	mux.Handle("/onvif/media_service", handler)

	// In proxy mode, UniFi Protect fetches the snapshot from THIS host, so
	// serve it here by proxying to the camera. Registered before the "/"
	// catch-all; ServeMux prefers the more specific route.
	if cfg.Mode == "proxy" {
		snapRoute := onvif.SnapshotRoute(cfg)
		mux.HandleFunc(snapRoute, onvif.SnapshotHandler(cfg, logger))
		logger.Printf("snapshot proxy: serving %s -> camera %s:%d", snapRoute, cfg.Target.Host, cfg.Target.SnapshotPort)
	}

	// Be lenient: dispatch is by SOAP body action name, not path, so also
	// accept requests at the root in case a client is misconfigured.
	mux.Handle("/", handler)

	httpServer := &http.Server{
		Addr:    cfg.Listen,
		Handler: mux,
		// Timeouts bound how long a single connection can tie up a goroutine
		// and file descriptor. Without them a slow or stuck client (or a
		// slowloris) pins resources forever — the resource-leak failure mode
		// this rewrite exists to avoid. ONVIF SOAP calls are tiny and fast,
		// so these limits are generous.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Printf("HTTP: listening on %s (onvif/device_service, onvif/media_service)", cfg.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("HTTP server failed: %v", err)
		}
	}()

	if cfg.Mode == "proxy" {
		target := fmt.Sprintf("%s:%d", cfg.Target.Host, cfg.Target.RTSPPort)
		rtspProxy, err := onvif.StartRTSPProxy(cfg.Proxy.RTSPListen, target, logger)
		if err != nil {
			logger.Fatalf("RTSP proxy failed to start: %v", err)
		}
		logger.Printf("RTSP proxy: listening on %s -> camera %s", cfg.Proxy.RTSPListen, target)
		defer rtspProxy.Close()
	}

	discovery, err := onvif.StartDiscoveryResponder(cfg, logger)
	if err != nil {
		// WS-Discovery is a nice-to-have — UniFi Protect's "Advanced
		// Adoption" doesn't need it. Log and keep running without it
		// rather than aborting startup.
		logger.Printf("WS-Discovery: could not start (continuing without it): %v", err)
	} else {
		logger.Printf("WS-Discovery: listening for Probe messages on %s", "239.255.255.250:3702")
		defer discovery.Close()
	}

	waitForShutdown(logger)
}

// startMemStatsLogger logs a snapshot of the process's memory usage now and
// then every interval, so leaks show up as steadily-climbing heap or
// goroutine counts over hours/days. A flat line here is the signal that the
// old Node server's leak class is gone. interval <= 0 disables it.
func startMemStatsLogger(logger *log.Logger, interval time.Duration) {
	logMemStats(logger) // baseline immediately, don't wait a full interval
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			logMemStats(logger)
		}
	}()
}

func logMemStats(logger *log.Logger) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Printf("MEMSTATS heapAlloc=%s heapInuse=%s heapObjects=%d stackInuse=%s sys=%s goroutines=%d numGC=%d",
		humanBytes(m.HeapAlloc), humanBytes(m.HeapInuse), m.HeapObjects,
		humanBytes(m.StackInuse), humanBytes(m.Sys), runtime.NumGoroutine(), m.NumGC)
}

// humanBytes formats a byte count as a compact human-readable string.
func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// waitForShutdown blocks until SIGINT or SIGTERM is received.
func waitForShutdown(logger *log.Logger) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh
	logger.Printf("received signal %s, shutting down", sig)
}

// setupLogging opens the log file (creating it if needed) and returns a
// *log.Logger that writes to both it and stderr, plus a closer to call on
// shutdown.
func setupLogging(path string) (*log.Logger, func(), error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file %q: %w", path, err)
	}
	writer := io.MultiWriter(os.Stderr, f)
	logger := log.New(writer, "", log.Ldate|log.Ltime|log.Lmicroseconds)
	return logger, func() { f.Close() }, nil
}

// logStartupConfig logs the effective configuration (with secrets
// redacted) and the listen addresses in use.
func logStartupConfig(logger *log.Logger, cfg *config.Config, configPath, logPath string) {
	logger.Printf("starting reolink-onvif-shim (config=%s log=%s)", configPath, logPath)
	redacted, err := json.MarshalIndent(cfg.Redacted(), "", "  ")
	if err != nil {
		logger.Printf("warning: could not marshal config for logging: %v", err)
		return
	}
	logger.Printf("effective config (passwords redacted):\n%s", redacted)
	logger.Printf("listen addresses: HTTP=%s WS-Discovery=239.255.255.250:3702", cfg.Listen)
}
