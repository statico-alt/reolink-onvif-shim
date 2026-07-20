package onvif

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// TestSnapshotHandler_ProxiesJPEG stands up a fake camera and verifies the
// snapshot handler fetches from it and streams the bytes + content-type
// back to the caller.
func TestSnapshotHandler_ProxiesJPEG(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xDB, 'F', 'A', 'K', 'E'}
	var gotQuery string
	cam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpeg)
	}))
	defer cam.Close()

	host, port := hostPortFromURL(t, cam.URL)
	cfg := testConfig()
	cfg.Target.Host = host
	cfg.Target.SnapshotPort = port

	h := SnapshotHandler(cfg, nil)
	// Protect may send any query; the handler rebuilds the upstream URL
	// from config, so credentials are always correct regardless.
	req := httptest.NewRequest(http.MethodGet, "/cgi-bin/api.cgi?whatever=1", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if got := rr.Body.Bytes(); string(got) != string(jpeg) {
		t.Errorf("body = %v, want forwarded jpeg %v", got, jpeg)
	}
	// The handler must use the config's snapshot query + creds, not the
	// caller's.
	if wantSub := "cmd=Snap"; !contains(gotQuery, wantSub) {
		t.Errorf("upstream query %q missing %q", gotQuery, wantSub)
	}
	if wantSub := "password=reolinkpass"; !contains(gotQuery, wantSub) {
		t.Errorf("upstream query %q missing camera creds %q", gotQuery, wantSub)
	}
}

// TestRTSPProxy_ForwardsBothDirections proves the TCP proxy relays bytes
// client->camera and camera->client, using an echo upstream.
func TestRTSPProxy_ForwardsBothDirections(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("upstream listen: %v", err)
	}
	defer upstream.Close()
	go func() {
		for {
			c, err := upstream.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); io.Copy(c, c) }(c) // echo
		}
	}()

	p, err := StartRTSPProxy("127.0.0.1:0", upstream.Addr().String(), nil)
	if err != nil {
		t.Fatalf("StartRTSPProxy: %v", err)
	}
	defer p.Close()

	conn, err := net.DialTimeout("tcp", p.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	msg := []byte("RTSP-PING")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echoed bytes through proxy: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("got %q through proxy, want %q", buf, msg)
	}
}

// TestRTSPProxy_DialFailureDoesNotCrash ensures a client connection is
// handled cleanly (closed, no panic) when the camera is unreachable.
func TestRTSPProxy_DialFailureDoesNotCrash(t *testing.T) {
	// Reserve a port then close it so dialing it is refused.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadAddr := dead.Addr().String()
	dead.Close()

	p, err := StartRTSPProxy("127.0.0.1:0", deadAddr, nil)
	if err != nil {
		t.Fatalf("StartRTSPProxy: %v", err)
	}
	defer p.Close()

	conn, err := net.DialTimeout("tcp", p.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	// The proxy should close our connection promptly since the upstream
	// dial fails; a read returns EOF rather than hanging or crashing.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Errorf("expected connection to be closed by proxy, got data")
	}
}

func hostPortFromURL(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port from %q: %v", raw, err)
	}
	return u.Hostname(), port
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
