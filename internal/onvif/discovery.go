package onvif

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/statico/reolink-onvif-shim/internal/config"
)

// WS-Discovery multicast group/port, per the WS-Discovery spec. UniFi
// Protect's "Advanced Adoption" doesn't need this (it adopts by unicast
// IP), but it's cheap to include and lets the camera also show up via
// ordinary ONVIF discovery tools.
const wsDiscoveryMulticastAddr = "239.255.255.250:3702"

const (
	nsWSD = "http://schemas.xmlsoap.org/ws/2005/04/discovery"
	nsWSA = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
	nsDN  = "http://www.onvif.org/ver10/network/wsdl"
)

// discoveryEnvelope is a lenient parse of an incoming WS-Discovery
// message: just enough to identify Probe requests and echo back the
// MessageID as RelatesTo.
type discoveryEnvelope struct {
	Header struct {
		Action    string `xml:"Action"`
		MessageID string `xml:"MessageID"`
	} `xml:"Header"`
	Body struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"Body"`
}

func parseDiscoveryEnvelope(raw []byte) (*discoveryEnvelope, error) {
	var env discoveryEnvelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsing WS-Discovery message: %w", err)
	}
	return &env, nil
}

// isProbe reports whether env is a WS-Discovery Probe request, checked by
// the wsa:Action URI suffix and, failing that, by the Body's first child
// element name.
func (e *discoveryEnvelope) isProbe() bool {
	if strings.HasSuffix(e.Header.Action, "/Probe") {
		return true
	}
	dec := xml.NewDecoder(bytes.NewReader(e.Body.Inner))
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local == "Probe"
		}
	}
}

// buildProbeMatch builds a WS-Discovery ProbeMatches response envelope.
func buildProbeMatch(relatesTo, uuid, xaddr string) []byte {
	return []byte(fmt.Sprintf(
		`<?xml version="1.0" encoding="UTF-8"?>`+
			`<soap:Envelope xmlns:soap="%s" xmlns:wsa="%s" xmlns:wsd="%s" xmlns:dn="%s">`+
			`<soap:Header>`+
			`<wsa:Action>%s/ProbeMatches</wsa:Action>`+
			`<wsa:RelatesTo>%s</wsa:RelatesTo>`+
			`</soap:Header>`+
			`<soap:Body>`+
			`<wsd:ProbeMatches>`+
			`<wsd:ProbeMatch>`+
			`<wsa:EndpointReference><wsa:Address>urn:uuid:%s</wsa:Address></wsa:EndpointReference>`+
			`<wsd:Types>dn:NetworkVideoTransmitter</wsd:Types>`+
			`<wsd:Scopes>onvif://www.onvif.org/type/video_encoder</wsd:Scopes>`+
			`<wsd:XAddrs>%s</wsd:XAddrs>`+
			`<wsd:MetadataVersion>1</wsd:MetadataVersion>`+
			`</wsd:ProbeMatch>`+
			`</wsd:ProbeMatches>`+
			`</soap:Body>`+
			`</soap:Envelope>`,
		NSSOAP, nsWSA, nsWSD, nsDN, nsWSD, relatesTo, uuid, xaddr,
	))
}

// HandleDiscoveryPacket parses a raw WS-Discovery UDP packet. If it's a
// Probe, it returns (response, true, nil) with response being the
// ProbeMatches reply to send back to the sender, advertising our device
// service at http://<host>/onvif/device_service. Non-Probe messages
// (Hello, Bye, etc.) are ignored: (nil, false, nil). Malformed input
// returns a non-nil error.
func HandleDiscoveryPacket(cfg *config.Config, raw []byte, host string) ([]byte, bool, error) {
	env, err := parseDiscoveryEnvelope(raw)
	if err != nil {
		return nil, false, err
	}
	if !env.isProbe() {
		return nil, false, nil
	}
	xaddr := baseURL(host, "/onvif/device_service")
	return buildProbeMatch(env.Header.MessageID, cfg.Device.UUID, xaddr), true, nil
}

// DiscoveryResponder listens for WS-Discovery Probe messages on the
// standard multicast group and answers them with a unicast ProbeMatch.
type DiscoveryResponder struct {
	conn *net.UDPConn
	cfg  *config.Config
	log  *log.Logger
}

// StartDiscoveryResponder joins the WS-Discovery multicast group and
// begins answering Probe messages in the background. Callers should treat
// a returned error as non-fatal: WS-Discovery is a nice-to-have (Advanced
// Adoption works without it), so the caller may choose to log and continue
// without it rather than aborting startup.
func StartDiscoveryResponder(cfg *config.Config, logger *log.Logger) (*DiscoveryResponder, error) {
	addr, err := net.ResolveUDPAddr("udp4", wsDiscoveryMulticastAddr)
	if err != nil {
		return nil, fmt.Errorf("resolving WS-Discovery multicast address: %w", err)
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("joining WS-Discovery multicast group: %w", err)
	}
	d := &DiscoveryResponder{conn: conn, cfg: cfg, log: logger}
	go d.loop()
	return d, nil
}

// Close stops the responder.
func (d *DiscoveryResponder) Close() error {
	return d.conn.Close()
}

func (d *DiscoveryResponder) logf(format string, args ...any) {
	if d.log == nil {
		return
	}
	d.log.Printf(format, args...)
}

func (d *DiscoveryResponder) loop() {
	buf := make([]byte, 65536)
	for {
		n, src, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			// Most likely the connection was closed during shutdown.
			d.logf("WS-Discovery: listener stopped: %v", err)
			return
		}
		raw := make([]byte, n)
		copy(raw, buf[:n])
		go d.handle(raw, src)
	}
}

// handle processes a single received datagram. It never panics: like the
// HTTP handler, any panic while handling a (possibly malformed) datagram is
// recovered and logged so one bad packet can't take down the process.
func (d *DiscoveryResponder) handle(raw []byte, src *net.UDPAddr) {
	defer func() {
		if rec := recover(); rec != nil {
			d.logf("WS-Discovery: PANIC handling packet from %s: %v", src, rec)
		}
	}()

	d.logf("WS-Discovery: probe received from %s (%d bytes)", src, len(raw))

	host, err := localIPFor(src)
	if err != nil {
		d.logf("WS-Discovery: could not determine local address to reply to %s: %v", src, err)
		return
	}
	_, port, err := net.SplitHostPort(d.cfg.Listen)
	if err != nil || port == "" {
		port = "80"
	}

	resp, handled, err := HandleDiscoveryPacket(d.cfg, raw, net.JoinHostPort(host, port))
	if err != nil {
		d.logf("WS-Discovery: error handling packet from %s: %v", src, err)
		return
	}
	if !handled {
		d.logf("WS-Discovery: ignoring non-Probe message from %s", src)
		return
	}
	if _, err := d.conn.WriteToUDP(resp, src); err != nil {
		d.logf("WS-Discovery: error sending ProbeMatch to %s: %v", src, err)
		return
	}
	d.logf("WS-Discovery: sent ProbeMatch to %s", src)
}

// localIPFor returns the local IP address that would be used to send
// traffic to remote, by opening (but not actually transmitting on) a UDP
// "connected" socket. This is the standard trick for discovering which
// local interface/IP faces a given peer, so our WS-Discovery replies
// advertise a reachable address rather than a wildcard.
func localIPFor(remote *net.UDPAddr) (string, error) {
	conn, err := net.Dial("udp4", remote.String())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected local address type %T", conn.LocalAddr())
	}
	return localAddr.IP.String(), nil
}
