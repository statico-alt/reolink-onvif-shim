package onvif

import (
	"crypto/sha1"
	"encoding/base64"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() (*Server, *httptest.Server) {
	svc := NewService(testConfig(), nil)
	logger := log.New(logSinkWriter{}, "", 0)
	srv := NewServer(svc, testConfig(), logger)
	ts := httptest.NewServer(srv)
	return srv, ts
}

// logSinkWriter discards log output during tests.
type logSinkWriter struct{}

func (logSinkWriter) Write(p []byte) (int, error) { return len(p), nil }

func digestFor(nonce []byte, created, password string) (nonceB64, digest string) {
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(password))
	return base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func authedEnvelope(action, username, password string) string {
	nonceB64, digest := digestFor([]byte("fixed-test-nonce"), "2026-07-19T12:00:00Z", password)
	return buildRawEnvelope(
		`<wsse:Security xmlns:wsse="`+NSWSSE+`" xmlns:wsu="`+NSWSU+`"><wsse:UsernameToken>`+
			`<wsse:Username>`+username+`</wsse:Username>`+
			`<wsse:Password Type="...#PasswordDigest">`+digest+`</wsse:Password>`+
			`<wsse:Nonce>`+nonceB64+`</wsse:Nonce>`+
			`<wsu:Created>2026-07-19T12:00:00Z</wsu:Created>`+
			`</wsse:UsernameToken></wsse:Security>`,
		`<tds:`+action+` xmlns:tds="`+NSTDS+`" xmlns:trt="`+NSTRT+`"/>`,
	)
}

func buildRawEnvelope(header, bodyInner string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<soap:Envelope xmlns:soap="` + NSSOAP + `">`)
	if header != "" {
		sb.WriteString("<soap:Header>" + header + "</soap:Header>")
	}
	sb.WriteString("<soap:Body>" + bodyInner + "</soap:Body>")
	sb.WriteString("</soap:Envelope>")
	return sb.String()
}

func postSOAP(t *testing.T, ts *httptest.Server, path, body string) *httptestResponse {
	t.Helper()
	resp, err := ts.Client().Post(ts.URL+path, "application/soap+xml; charset=utf-8", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		t.Fatalf("reading response body: %v", err)
	}
	return &httptestResponse{status: resp.StatusCode, contentType: resp.Header.Get("Content-Type"), body: buf.String()}
}

type httptestResponse struct {
	status      int
	contentType string
	body        string
}

func TestServer_GetSystemDateAndTime_NoAuthRequired(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	body := buildRawEnvelope("", `<tds:GetSystemDateAndTime xmlns:tds="`+NSTDS+`"/>`)
	resp := postSOAP(t, ts, "/onvif/device_service", body)
	if resp.status != 200 {
		t.Fatalf("status = %d, want 200. body: %s", resp.status, resp.body)
	}
	if !strings.Contains(resp.contentType, "application/soap+xml") {
		t.Errorf("Content-Type = %q, want application/soap+xml", resp.contentType)
	}
	if !strings.Contains(resp.body, "GetSystemDateAndTimeResponse") {
		t.Errorf("body missing GetSystemDateAndTimeResponse: %s", resp.body)
	}
	assertWellFormed(t, []byte(resp.body))
}

func TestServer_AuthRequiredAction_NoAuth_Fails(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	body := buildRawEnvelope("", `<tds:GetDeviceInformation xmlns:tds="`+NSTDS+`"/>`)
	resp := postSOAP(t, ts, "/onvif/device_service", body)
	if resp.status != 400 {
		t.Fatalf("status = %d, want 400. body: %s", resp.status, resp.body)
	}
	if !strings.Contains(resp.body, "Fault") {
		t.Errorf("body missing Fault: %s", resp.body)
	}
	if !strings.Contains(resp.body, "FailedAuthentication") {
		t.Errorf("body missing FailedAuthentication subcode: %s", resp.body)
	}
	assertWellFormed(t, []byte(resp.body))
}

func TestServer_AuthRequiredAction_GoodAuth_Succeeds(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	body := authedEnvelope("GetDeviceInformation", "protect", "supersecret")
	resp := postSOAP(t, ts, "/onvif/device_service", body)
	if resp.status != 200 {
		t.Fatalf("status = %d, want 200. body: %s", resp.status, resp.body)
	}
	if !strings.Contains(resp.body, "GetDeviceInformationResponse") {
		t.Errorf("body missing response: %s", resp.body)
	}
}

func TestServer_AuthRequiredAction_BadPassword_Fails(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	body := authedEnvelope("GetDeviceInformation", "protect", "wrongpass")
	resp := postSOAP(t, ts, "/onvif/device_service", body)
	if resp.status != 400 {
		t.Fatalf("status = %d, want 400. body: %s", resp.status, resp.body)
	}
}

func TestServer_UnknownAction_ReturnsFaultNot500(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	body := authedEnvelope("SomeTotallyUnknownAction", "protect", "supersecret")
	resp := postSOAP(t, ts, "/onvif/device_service", body)
	if resp.status == 500 {
		t.Fatalf("status = %d, want != 500 (unknown action must not surface as a server error): body: %s", resp.status, resp.body)
	}
	if resp.status != 400 {
		t.Errorf("status = %d, want 400. body: %s", resp.status, resp.body)
	}
	if resp.body == "" {
		t.Fatalf("body is empty, want a well-formed SOAP Fault")
	}
	if !strings.Contains(resp.body, "Fault") {
		t.Errorf("body missing Fault element: %s", resp.body)
	}
	if !strings.Contains(resp.body, "Subcode") {
		t.Errorf("body missing Fault Subcode: %s", resp.body)
	}
	if !strings.Contains(resp.body, "ActionNotSupported") {
		t.Errorf("body missing ActionNotSupported: %s", resp.body)
	}
	assertWellFormed(t, []byte(resp.body))
}

func TestServer_MalformedXML_DoesNotCrash(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	resp := postSOAP(t, ts, "/onvif/device_service", "<this is not xml")
	if resp.status < 400 || resp.status >= 600 {
		t.Fatalf("status = %d, want 4xx/5xx for malformed XML", resp.status)
	}
	assertWellFormed(t, []byte(resp.body))
}

func TestServer_DispatchesByBodyActionRegardlessOfPath(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	// GetProfiles is a "media" action; hitting the device_service path
	// should still work since dispatch is by body action name, not path.
	body := authedEnvelope("GetProfiles", "protect", "supersecret")
	resp := postSOAP(t, ts, "/onvif/device_service", body)
	if resp.status != 200 {
		t.Fatalf("status = %d, want 200. body: %s", resp.status, resp.body)
	}
	if !strings.Contains(resp.body, "GetProfilesResponse") {
		t.Errorf("body missing GetProfilesResponse: %s", resp.body)
	}
}

func TestServer_GetStreamUri_ReturnsRealRTSPUrl(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()
	body := authedEnvelope("GetStreamUri", "protect", "supersecret")
	resp := postSOAP(t, ts, "/onvif/media_service", body)
	if resp.status != 200 {
		t.Fatalf("status = %d, want 200. body: %s", resp.status, resp.body)
	}
	if !strings.Contains(resp.body, "rtsp://admin:reolinkpass@192.0.2.45:554/h264Preview_01_main") {
		t.Errorf("body missing stream URI: %s", resp.body)
	}
}
