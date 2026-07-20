package onvif

import (
	"strings"
	"testing"
)

func TestGetProfiles(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetProfiles()
	if err != nil {
		t.Fatalf("GetProfiles returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	for _, want := range []string{"MainStream", "VideoSourceConfig", "VideoEncoderConfig", "H264", "2560", "1920", "20", "4096"} {
		if !strings.Contains(str, want) {
			t.Errorf("GetProfiles response missing %q: %s", want, str)
		}
	}
}

func TestGetProfile(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetProfile("MainStream")
	if err != nil {
		t.Fatalf("GetProfile returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	if !strings.Contains(str, "MainStream") {
		t.Errorf("GetProfile response missing token: %s", str)
	}
}

func TestGetProfileUnknownToken(t *testing.T) {
	s := NewService(testConfig(), nil)
	_, err := s.GetProfile("NoSuchProfile")
	if err == nil {
		t.Fatal("expected error for unknown profile token, got nil")
	}
}

func TestGetVideoEncoderConfigurations(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetVideoEncoderConfigurations()
	if err != nil {
		t.Fatalf("GetVideoEncoderConfigurations returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	for _, want := range []string{"VideoEncoderConfig", "2560", "1920", "20", "4096"} {
		if !strings.Contains(str, want) {
			t.Errorf("response missing %q: %s", want, str)
		}
	}
}

func TestGetVideoEncoderConfiguration(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetVideoEncoderConfiguration()
	if err != nil {
		t.Fatalf("GetVideoEncoderConfiguration returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	if !strings.Contains(str, "2560") || !strings.Contains(str, "1920") {
		t.Errorf("response missing resolution: %s", str)
	}
}

func TestGetVideoSources(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetVideoSources()
	if err != nil {
		t.Fatalf("GetVideoSources returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	if !strings.Contains(str, "VideoSource") || !strings.Contains(str, "2560") {
		t.Errorf("response missing video source info: %s", str)
	}
}

func TestGetStreamUri(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetStreamUri("198.51.100.33:80")
	if err != nil {
		t.Fatalf("GetStreamUri returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	want := "rtsp://admin:reolinkpass@192.0.2.45:554/h264Preview_01_main"
	if !strings.Contains(str, want) {
		t.Errorf("response missing stream URI %q: %s", want, str)
	}
	if !strings.Contains(str, "<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect>") {
		t.Errorf("response missing InvalidAfterConnect: %s", str)
	}
	if !strings.Contains(str, "<tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>") {
		t.Errorf("response missing InvalidAfterReboot: %s", str)
	}
	if !strings.Contains(str, "<tt:Timeout>PT0S</tt:Timeout>") {
		t.Errorf("response missing Timeout: %s", str)
	}
}

func TestGetSnapshotUri(t *testing.T) {
	s := NewService(testConfig(), nil)
	body, err := s.GetSnapshotUri("198.51.100.33:80")
	if err != nil {
		t.Fatalf("GetSnapshotUri returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	want := "http://192.0.2.45:80/cgi-bin/api.cgi?cmd=Snap&amp;channel=0&amp;user=admin&amp;password=reolinkpass"
	if !strings.Contains(str, want) {
		t.Errorf("response missing snapshot URI (xml-escaped) %q: %s", want, str)
	}
}

func TestGetStreamUri_ProxyMode(t *testing.T) {
	cfg := testConfig()
	cfg.Mode = "proxy"
	cfg.Proxy.RTSPListen = ":8554"
	s := NewService(cfg, nil)
	body, err := s.GetStreamUri("198.51.100.33:80")
	if err != nil {
		t.Fatalf("GetStreamUri returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	// In proxy mode the URI must point back at THIS host (the request host)
	// on the proxy port, not at the camera — Protect fetches media from the
	// device IP, so the camera's own address would never be reached.
	want := "rtsp://admin:reolinkpass@198.51.100.33:8554/h264Preview_01_main"
	if !strings.Contains(str, want) {
		t.Errorf("proxy-mode stream URI wrong, want %q: %s", want, str)
	}
	if strings.Contains(str, "192.0.2.45") {
		t.Errorf("proxy-mode stream URI must not expose the camera address: %s", str)
	}
}

func TestGetSnapshotUri_ProxyMode(t *testing.T) {
	cfg := testConfig()
	cfg.Mode = "proxy"
	cfg.Listen = ":80"
	s := NewService(cfg, nil)
	body, err := s.GetSnapshotUri("198.51.100.33:80")
	if err != nil {
		t.Fatalf("GetSnapshotUri returned error: %v", err)
	}
	assertWellFormed(t, body)
	str := string(body)
	// Points at this host's snapshot proxy (main HTTP port), not the camera.
	want := "http://198.51.100.33:80/cgi-bin/api.cgi?cmd=Snap&amp;channel=0&amp;user=admin&amp;password=reolinkpass"
	if !strings.Contains(str, want) {
		t.Errorf("proxy-mode snapshot URI wrong, want %q: %s", want, str)
	}
	if strings.Contains(str, "192.0.2.45") {
		t.Errorf("proxy-mode snapshot URI must not expose the camera address: %s", str)
	}
}
