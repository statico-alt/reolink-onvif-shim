package onvif

import (
	"strings"
	"testing"
	"time"

	"github.com/statico-alt/reolink-onvif-shim/internal/config"
)

func testConfig() *config.Config {
	var cfg config.Config
	cfg.Listen = ":8080"
	cfg.Mode = "direct"
	cfg.ONVIF.Username = "protect"
	cfg.ONVIF.Password = "supersecret"
	cfg.Device.Manufacturer = "Reolink"
	cfg.Device.Model = "Video Doorbell"
	cfg.Device.Firmware = "v1.0.0"
	cfg.Device.Serial = "ABC123"
	cfg.Device.UUID = "12345678-1234-1234-1234-123456789abc"
	cfg.Target.Host = "192.0.2.45"
	cfg.Target.RTSPPort = 554
	cfg.Target.SnapshotPort = 80
	cfg.Target.Username = "admin"
	cfg.Target.Password = "reolinkpass"
	cfg.Stream.RTSPPath = "/h264Preview_01_main"
	cfg.Stream.SnapshotPath = "/cgi-bin/api.cgi?cmd=Snap&channel=0"
	cfg.Stream.Width = 2560
	cfg.Stream.Height = 1920
	cfg.Stream.Framerate = 20
	cfg.Stream.Bitrate = 4096
	return &cfg
}

func assertWellFormed(t *testing.T, body []byte) {
	t.Helper()
	if err := xmlWellFormed(body); err != nil {
		t.Fatalf("response is not well-formed XML: %v\n---\n%s", err, body)
	}
}

func TestGetSystemDateAndTime(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetSystemDateAndTime()
	if err != nil {
		t.Fatalf("GetSystemDateAndTime returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	if !strings.Contains(str, "GetSystemDateAndTimeResponse") {
		t.Errorf("missing GetSystemDateAndTimeResponse: %s", str)
	}
	if !strings.Contains(str, "Manual") {
		t.Errorf("missing DateTimeType Manual: %s", str)
	}
	if !strings.Contains(str, "<tt:UTCDateTime>") {
		t.Errorf("missing tt:UTCDateTime: %s", str)
	}
	year := strings.TrimSpace(time.Now().UTC().Format("2006"))
	if !strings.Contains(str, year) {
		t.Errorf("missing current year %s in response: %s", year, str)
	}
}

func TestGetDeviceInformation(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetDeviceInformation()
	if err != nil {
		t.Fatalf("GetDeviceInformation returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	for _, want := range []string{"Reolink", "Video Doorbell", "v1.0.0", "ABC123"} {
		if !strings.Contains(str, want) {
			t.Errorf("response missing %q: %s", want, str)
		}
	}
}

func TestGetCapabilities(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetCapabilities("198.51.100.33:8080")
	if err != nil {
		t.Fatalf("GetCapabilities returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	if !strings.Contains(str, "http://198.51.100.33:8080/onvif/device_service") {
		t.Errorf("missing device XAddr: %s", str)
	}
	if !strings.Contains(str, "http://198.51.100.33:8080/onvif/media_service") {
		t.Errorf("missing media XAddr: %s", str)
	}
}

func TestGetServices(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetServices("198.51.100.33:8080")
	if err != nil {
		t.Fatalf("GetServices returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	if !strings.Contains(str, NSTDS) || !strings.Contains(str, NSTRT) {
		t.Errorf("missing service namespaces: %s", str)
	}
	if !strings.Contains(str, "http://198.51.100.33:8080/onvif/device_service") {
		t.Errorf("missing device XAddr: %s", str)
	}
	if !strings.Contains(str, "http://198.51.100.33:8080/onvif/media_service") {
		t.Errorf("missing media XAddr: %s", str)
	}
}

func TestGetScopes(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetScopes()
	if err != nil {
		t.Fatalf("GetScopes returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	for _, want := range []string{
		"onvif://www.onvif.org/type/video_encoder",
		"onvif://www.onvif.org/name/Video Doorbell",
		"onvif://www.onvif.org/hardware/Video Doorbell",
		"onvif://www.onvif.org/location/",
	} {
		if !strings.Contains(str, want) {
			t.Errorf("response missing scope %q: %s", want, str)
		}
	}
}
