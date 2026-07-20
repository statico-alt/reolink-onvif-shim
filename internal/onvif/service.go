package onvif

import (
	"log"

	"github.com/statico/reolink-onvif-shim/internal/config"
)

// Service implements the ONVIF Device and Media SOAP handlers. Its methods
// build and marshal individual SOAP response bodies (wrapped in a full
// envelope); HTTP transport, auth enforcement, and logging live in
// server.go.
type Service struct {
	cfg *config.Config
	log *log.Logger
}

// NewService constructs a Service backed by cfg. logger may be nil, in
// which case Service methods that would otherwise log (currently none —
// logging happens at the server/dispatch layer) are silently no-ops.
func NewService(cfg *config.Config, logger *log.Logger) *Service {
	return &Service{cfg: cfg, log: logger}
}

// baseURL builds a "http://<host>/onvif/<path>" XAddr using the Host the
// client used to reach us (from the inbound request's Host header), so the
// advertised service address matches how the client actually connected.
func baseURL(host, path string) string {
	return "http://" + host + path
}
