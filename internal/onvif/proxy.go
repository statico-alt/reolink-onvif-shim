package onvif

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/statico-alt/reolink-onvif-shim/internal/config"
)

// SnapshotRoute returns just the path portion of the configured snapshot
// path (everything before any '?'), e.g. "/cgi-bin/api.cgi". This is the
// route the snapshot proxy handler registers.
func SnapshotRoute(cfg *config.Config) string {
	p := cfg.Stream.SnapshotPath
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

// SnapshotHandler returns an http.HandlerFunc that fetches a JPEG snapshot
// from the real camera and streams it back. UniFi Protect requests the
// snapshot from THIS host (it ignores the host in the ONVIF snapshot URI),
// so in proxy mode we serve it here. The upstream URL — including the
// Reolink credentials as query params — is rebuilt from config, so it works
// regardless of what query Protect sends.
func SnapshotHandler(cfg *config.Config, logger *log.Logger) http.HandlerFunc {
	upstream := fmt.Sprintf("http://%s:%d%s&user=%s&password=%s",
		cfg.Target.Host, cfg.Target.SnapshotPort, cfg.Stream.SnapshotPath,
		cfg.Target.Username, cfg.Target.Password)

	client := &http.Client{Timeout: 15 * time.Second}

	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil && logger != nil {
				logger.Printf("SNAPSHOT proxy panic (remote=%s): %v", r.RemoteAddr, rec)
			}
		}()

		resp, err := client.Get(upstream)
		if err != nil {
			if logger != nil {
				logger.Printf("SNAPSHOT proxy: fetch from camera failed (remote=%s): %v", r.RemoteAddr, err)
			}
			http.Error(w, "snapshot upstream error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
		n, _ := io.Copy(w, resp.Body)
		if logger != nil {
			logger.Printf("SNAPSHOT proxy: served %d bytes to %s (upstream status %d)", n, r.RemoteAddr, resp.StatusCode)
		}
	}
}

// RTSPProxy is a plain TCP proxy: it accepts RTSP connections from UniFi
// Protect and forwards the bytes to the real camera. Protect uses RTP over
// the same TCP connection (interleaved), so a byte-for-byte TCP proxy is
// sufficient and no video is ever parsed or buffered beyond the OS socket.
type RTSPProxy struct {
	ln     net.Listener
	target string
	log    *log.Logger
}

// StartRTSPProxy binds listen (e.g. ":8554") and forwards accepted
// connections to target ("host:port"). It returns immediately; accepting
// runs in the background until Close is called.
func StartRTSPProxy(listen, target string, logger *log.Logger) (*RTSPProxy, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("RTSP proxy listen on %s: %w", listen, err)
	}
	p := &RTSPProxy{ln: ln, target: target, log: logger}
	go p.acceptLoop()
	return p, nil
}

// Addr returns the address the proxy is listening on.
func (p *RTSPProxy) Addr() net.Addr { return p.ln.Addr() }

// Close stops accepting new connections. In-flight connections are left to
// drain and close on their own.
func (p *RTSPProxy) Close() error { return p.ln.Close() }

func (p *RTSPProxy) acceptLoop() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			// Accept fails permanently once the listener is closed; that's
			// the normal shutdown path, so just stop looping.
			return
		}
		go p.handle(client)
	}
}

// handle proxies one client connection to the camera. It never panics out
// to the accept loop, and it never leaks goroutines or file descriptors:
// both connections are closed on return, which unblocks both copy
// directions.
func (p *RTSPProxy) handle(client net.Conn) {
	defer func() {
		if rec := recover(); rec != nil {
			p.logf("RTSP proxy panic (client=%s): %v", client.RemoteAddr(), rec)
		}
	}()
	defer client.Close()

	upstream, err := net.DialTimeout("tcp", p.target, 10*time.Second)
	if err != nil {
		p.logf("RTSP proxy: dial camera %s failed (client=%s): %v", p.target, client.RemoteAddr(), err)
		return
	}
	defer upstream.Close()

	p.logf("RTSP proxy: connected client=%s <-> camera=%s", client.RemoteAddr(), p.target)

	// Copy in both directions. The channel is buffered for both goroutines
	// so the second one to finish never blocks writing to it after handle
	// has already returned — no goroutine leak.
	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, client); done <- struct{}{} }()
	go func() { io.Copy(client, upstream); done <- struct{}{} }()

	// As soon as either direction ends, return. The deferred Close calls
	// tear down both sockets, which makes the other io.Copy return too.
	<-done
	p.logf("RTSP proxy: session ended client=%s", client.RemoteAddr())
}

func (p *RTSPProxy) logf(format string, args ...any) {
	if p.log == nil {
		return
	}
	p.log.Printf(format, args...)
}
