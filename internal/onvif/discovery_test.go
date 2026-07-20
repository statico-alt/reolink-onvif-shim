package onvif

import (
	"log"
	"net"
	"strings"
	"testing"
)

func buildDiscoveryProbe(t *testing.T, messageID string) []byte {
	t.Helper()
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<soap:Envelope xmlns:soap="` + NSSOAP + `" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:wsd="http://schemas.xmlsoap.org/ws/2005/04/discovery">` +
		`<soap:Header>` +
		`<wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</wsa:Action>` +
		`<wsa:MessageID>` + messageID + `</wsa:MessageID>` +
		`</soap:Header>` +
		`<soap:Body><wsd:Probe><wsd:Types>dn:NetworkVideoTransmitter</wsd:Types></wsd:Probe></soap:Body>` +
		`</soap:Envelope>`)
}

func TestHandleDiscoveryPacket_Probe(t *testing.T) {
	cfg := testConfig()
	messageID := "urn:uuid:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	raw := buildDiscoveryProbe(t, messageID)

	resp, handled, err := HandleDiscoveryPacket(cfg, raw, "198.51.100.33:8080")
	if err != nil {
		t.Fatalf("HandleDiscoveryPacket returned error: %v", err)
	}
	if !handled {
		t.Fatal("HandleDiscoveryPacket did not recognize a Probe message")
	}
	assertWellFormed(t, resp)
	str := string(resp)
	if !strings.Contains(str, "ProbeMatches") {
		t.Errorf("response missing ProbeMatches: %s", str)
	}
	if !strings.Contains(str, messageID) {
		t.Errorf("response missing RelatesTo message id %q: %s", messageID, str)
	}
	if !strings.Contains(str, cfg.Device.UUID) {
		t.Errorf("response missing device UUID %q: %s", cfg.Device.UUID, str)
	}
	if !strings.Contains(str, "http://198.51.100.33:8080/onvif/device_service") {
		t.Errorf("response missing device service XAddr: %s", str)
	}
}

func TestHandleDiscoveryPacket_IgnoresNonProbe(t *testing.T) {
	cfg := testConfig()
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<soap:Envelope xmlns:soap="` + NSSOAP + `" xmlns:wsa="http://schemas.xmlsoap.org/ws/2004/08/addressing">` +
		`<soap:Header><wsa:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/Hello</wsa:Action></soap:Header>` +
		`<soap:Body><wsd:Hello xmlns:wsd="http://schemas.xmlsoap.org/ws/2005/04/discovery"/></soap:Body>` +
		`</soap:Envelope>`)

	_, handled, err := HandleDiscoveryPacket(cfg, raw, "198.51.100.33:8080")
	if err != nil {
		t.Fatalf("HandleDiscoveryPacket returned error: %v", err)
	}
	if handled {
		t.Error("HandleDiscoveryPacket should not treat a Hello message as a Probe")
	}
}

func TestHandleDiscoveryPacket_MalformedInput(t *testing.T) {
	cfg := testConfig()
	_, _, err := HandleDiscoveryPacket(cfg, []byte("not xml at all"), "198.51.100.33:8080")
	if err == nil {
		t.Fatal("expected error for malformed WS-Discovery packet, got nil")
	}
}

// TestDiscoveryResponder_Handle_MalformedDatagram_DoesNotPanic exercises the
// per-datagram handling path (DiscoveryResponder.handle, invoked per packet
// from the UDP read loop) directly with malformed input, guarding the
// project's "never crash on bad input" requirement for the WS-Discovery
// listener the same way the HTTP handler is guarded.
func TestDiscoveryResponder_Handle_MalformedDatagram_DoesNotPanic(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer conn.Close()

	d := &DiscoveryResponder{
		conn: conn,
		cfg:  testConfig(),
		log:  log.New(logSinkWriter{}, "", 0),
	}
	src := conn.LocalAddr().(*net.UDPAddr)

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("handle panicked on malformed datagram: %v", rec)
		}
	}()

	malformed := [][]byte{
		[]byte("not xml at all"),
		[]byte("<this is not even close to well-formed"),
		{0x00, 0x01, 0x02, 0xff, 0xfe},
		[]byte(""),
	}
	for _, raw := range malformed {
		d.handle(raw, src)
	}
}
