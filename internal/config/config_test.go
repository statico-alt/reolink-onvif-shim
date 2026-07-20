package config

import (
	"os"
	"path/filepath"
	"testing"
)

const validJSON = `{
  "Listen": ":8080",
  "Debug": true,
  "Mode": "direct",
  "ONVIF": { "Username": "protect", "Password": "supersecret" },
  "Device": {
    "Manufacturer": "Reolink",
    "Model": "Video Doorbell",
    "Firmware": "v1.0.0",
    "Serial": "ABC123",
    "UUID": "12345678-1234-1234-1234-123456789abc"
  },
  "Target": {
    "Host": "192.0.2.45",
    "RTSPPort": 554,
    "SnapshotPort": 80,
    "Username": "admin",
    "Password": "reolinkpass"
  },
  "Stream": {
    "RTSPPath": "/h264Preview_01_main",
    "SnapshotPath": "/cgi-bin/api.cgi?cmd=Snap&channel=0",
    "Width": 2560,
    "Height": 1920,
    "Framerate": 20,
    "Bitrate": 4096
  }
}`

func writeTemp(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadValidConfig(t *testing.T) {
	path := writeTemp(t, validJSON)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}
	if !cfg.Debug {
		t.Errorf("Debug = false, want true")
	}
	if cfg.Mode != "direct" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "direct")
	}
	if cfg.ONVIF.Username != "protect" || cfg.ONVIF.Password != "supersecret" {
		t.Errorf("ONVIF creds mismatch: %+v", cfg.ONVIF)
	}
	if cfg.Device.Manufacturer != "Reolink" || cfg.Device.Model != "Video Doorbell" {
		t.Errorf("Device mismatch: %+v", cfg.Device)
	}
	if cfg.Target.Host != "192.0.2.45" || cfg.Target.RTSPPort != 554 || cfg.Target.SnapshotPort != 80 {
		t.Errorf("Target mismatch: %+v", cfg.Target)
	}
	if cfg.Stream.Width != 2560 || cfg.Stream.Height != 1920 || cfg.Stream.Framerate != 20 || cfg.Stream.Bitrate != 4096 {
		t.Errorf("Stream mismatch: %+v", cfg.Stream)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := writeTemp(t, `{ not valid json`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoadRejectsUnsupportedMode(t *testing.T) {
	path := writeTemp(t, `{
		"Listen": ":8080",
		"Mode": "bogus",
		"ONVIF": {"Username": "protect", "Password": "x"},
		"Target": {"Host": "192.0.2.45"}
	}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unsupported mode, got nil")
	}
}

func TestLoadProxyModeDefaultsRTSPListen(t *testing.T) {
	path := writeTemp(t, `{
		"Listen": ":8080",
		"Mode": "proxy",
		"ONVIF": {"Username": "protect", "Password": "x"},
		"Target": {"Host": "192.0.2.45"}
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("proxy mode should be accepted: %v", err)
	}
	if cfg.Proxy.RTSPListen != ":8554" {
		t.Errorf("Proxy.RTSPListen = %q, want default \":8554\"", cfg.Proxy.RTSPListen)
	}
}

func TestLoadDefaultsMode(t *testing.T) {
	// Mode omitted should default to "direct" rather than erroring.
	path := writeTemp(t, `{
		"Listen": ":8080",
		"ONVIF": {"Username": "protect", "Password": "x"},
		"Target": {"Host": "192.0.2.45"}
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Mode != "direct" {
		t.Errorf("Mode = %q, want default %q", cfg.Mode, "direct")
	}
}

func TestLoadCaseInsensitiveKeys(t *testing.T) {
	// The design doc's example config.json uses lowercase JSON keys; make sure
	// those load correctly too since encoding/json matches case-insensitively.
	path := writeTemp(t, `{
		"listen": ":8080",
		"mode": "direct",
		"onvif": {"username": "protect", "password": "supersecret"},
		"target": {"host": "192.0.2.45", "rtspPort": 554, "snapshotPort": 80, "username": "admin", "password": "reolinkpass"},
		"stream": {"rtspPath": "/h264Preview_01_main", "snapshotPath": "/snap", "width": 2560, "height": 1920, "framerate": 20, "bitrate": 4096}
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ONVIF.Username != "protect" || cfg.Target.Host != "192.0.2.45" {
		t.Errorf("case-insensitive load failed: %+v", cfg)
	}
}

func TestRedacted(t *testing.T) {
	path := writeTemp(t, validJSON)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	red := cfg.Redacted()
	if red.ONVIF.Password != "***" {
		t.Errorf("ONVIF.Password not redacted: %q", red.ONVIF.Password)
	}
	if red.Target.Password != "***" {
		t.Errorf("Target.Password not redacted: %q", red.Target.Password)
	}
	// Original must be unmodified.
	if cfg.ONVIF.Password != "supersecret" {
		t.Errorf("Redacted() mutated the original config")
	}
	if red.ONVIF.Username != "protect" || red.Target.Host != "192.0.2.45" {
		t.Errorf("Redacted() should not touch non-secret fields: %+v", red)
	}
}
