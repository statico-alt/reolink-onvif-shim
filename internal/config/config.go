// Package config loads and validates the reolink-onvif-shim JSON config file.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config is the top-level configuration for the shim.
type Config struct {
	Listen string `json:"Listen"`
	Debug  bool   `json:"Debug"`
	Mode   string `json:"Mode"`

	ONVIF struct {
		Username string `json:"Username"`
		Password string `json:"Password"`
	} `json:"ONVIF"`

	Device struct {
		Manufacturer string `json:"Manufacturer"`
		Model        string `json:"Model"`
		Firmware     string `json:"Firmware"`
		Serial       string `json:"Serial"`
		UUID         string `json:"UUID"`
	} `json:"Device"`

	Target struct {
		Host         string `json:"Host"`
		RTSPPort     int    `json:"RTSPPort"`
		SnapshotPort int    `json:"SnapshotPort"`
		Username     string `json:"Username"`
		Password     string `json:"Password"`
	} `json:"Target"`

	Stream struct {
		RTSPPath     string `json:"RTSPPath"`
		SnapshotPath string `json:"SnapshotPath"`
		Width        int    `json:"Width"`
		Height       int    `json:"Height"`
		Framerate    int    `json:"Framerate"`
		Bitrate      int    `json:"Bitrate"`
	} `json:"Stream"`

	// Proxy configures "proxy" mode. UniFi Protect ignores the host in the
	// stream/snapshot URIs we return and instead fetches media from this
	// device's own IP, so in proxy mode we listen locally and forward to
	// the camera. RTSPListen is where the RTSP TCP proxy listens (a high
	// port avoids needing root); the snapshot proxy is served on the main
	// HTTP Listen port.
	Proxy struct {
		RTSPListen string `json:"RTSPListen"`
	} `json:"Proxy"`
}

const redactedValue = "***"

// Load reads and parses the config file at path, applies defaults, and
// validates it. Mode defaults to "direct" if unset; any other value is
// rejected since only direct mode is implemented.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.Mode == "" {
		cfg.Mode = "direct"
	}
	if cfg.Mode != "direct" && cfg.Mode != "proxy" {
		return nil, fmt.Errorf("config %q: unsupported mode %q (want \"direct\" or \"proxy\")", path, cfg.Mode)
	}
	if cfg.Mode == "proxy" && cfg.Proxy.RTSPListen == "" {
		cfg.Proxy.RTSPListen = ":8554"
	}

	return &cfg, nil
}

// Redacted returns a copy of the config with secret fields (ONVIF.Password,
// Target.Password) replaced with a placeholder, suitable for logging.
func (c *Config) Redacted() *Config {
	cp := *c
	cp.ONVIF.Password = redactedValue
	cp.Target.Password = redactedValue
	return &cp
}
