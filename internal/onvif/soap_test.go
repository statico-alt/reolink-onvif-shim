package onvif

import (
	"crypto/sha1"
	"encoding/base64"
	"strings"
	"testing"
)

func computeExpectedDigest(t *testing.T, nonceRaw []byte, created, password string) string {
	t.Helper()
	h := sha1.New()
	h.Write(nonceRaw)
	h.Write([]byte(created))
	h.Write([]byte(password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestComputeDigest(t *testing.T) {
	nonceRaw := []byte("some-random-nonce-bytes")
	nonceB64 := base64.StdEncoding.EncodeToString(nonceRaw)
	created := "2026-07-19T12:00:00Z"
	password := "supersecret"

	want := computeExpectedDigest(t, nonceRaw, created, password)
	got, err := ComputeDigest(nonceB64, created, password)
	if err != nil {
		t.Fatalf("ComputeDigest returned error: %v", err)
	}
	if got != want {
		t.Errorf("ComputeDigest() = %q, want %q", got, want)
	}
}

func TestComputeDigestInvalidNonce(t *testing.T) {
	_, err := ComputeDigest("not-valid-base64!!!", "2026-07-19T12:00:00Z", "pw")
	if err == nil {
		t.Fatal("expected error for invalid base64 nonce, got nil")
	}
}

func buildEnvelope(t *testing.T, header, bodyInner string) []byte {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">`)
	if header != "" {
		sb.WriteString("<soap:Header>")
		sb.WriteString(header)
		sb.WriteString("</soap:Header>")
	}
	sb.WriteString("<soap:Body>")
	sb.WriteString(bodyInner)
	sb.WriteString("</soap:Body>")
	sb.WriteString("</soap:Envelope>")
	return []byte(sb.String())
}

func digestSecurityHeader(nonceB64, created, digest string) string {
	return `<wsse:Security><wsse:UsernameToken>` +
		`<wsse:Username>protect</wsse:Username>` +
		`<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` + digest + `</wsse:Password>` +
		`<wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` + nonceB64 + `</wsse:Nonce>` +
		`<wsu:Created>` + created + `</wsu:Created>` +
		`</wsse:UsernameToken></wsse:Security>`
}

func TestParseEnvelopeActionName(t *testing.T) {
	body := buildEnvelope(t, "", "<tds:GetSystemDateAndTime/>")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	action, err := env.Action()
	if err != nil {
		t.Fatalf("Action() returned error: %v", err)
	}
	if action != "GetSystemDateAndTime" {
		t.Errorf("Action() = %q, want %q", action, "GetSystemDateAndTime")
	}
}

func TestParseEnvelopeActionNameWithArgs(t *testing.T) {
	body := buildEnvelope(t, "", `<trt:GetProfile xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:ProfileToken>MainStream</trt:ProfileToken></trt:GetProfile>`)
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	action, err := env.Action()
	if err != nil {
		t.Fatalf("Action() returned error: %v", err)
	}
	if action != "GetProfile" {
		t.Errorf("Action() = %q, want %q", action, "GetProfile")
	}
}

func TestParseEnvelopeEmptyBody(t *testing.T) {
	body := buildEnvelope(t, "", "")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	if _, err := env.Action(); err == nil {
		t.Fatal("expected error determining action of empty body, got nil")
	}
}

func TestParseEnvelopeMalformedXML(t *testing.T) {
	_, err := ParseEnvelope([]byte("<soap:Envelope><not closed"))
	if err == nil {
		t.Fatal("expected error for malformed XML, got nil")
	}
}

func TestParseEnvelopeSecurityHeader(t *testing.T) {
	nonceRaw := []byte("randombytes123")
	nonceB64 := base64.StdEncoding.EncodeToString(nonceRaw)
	created := "2026-07-19T12:00:00Z"
	digest := computeExpectedDigest(t, nonceRaw, created, "supersecret")

	body := buildEnvelope(t, digestSecurityHeader(nonceB64, created, digest), "<tds:GetDeviceInformation/>")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	tok := env.Header.Security.UsernameToken
	if tok.Username != "protect" {
		t.Errorf("Username = %q, want %q", tok.Username, "protect")
	}
	if tok.Password.Value != digest {
		t.Errorf("Password.Value = %q, want %q", tok.Password.Value, digest)
	}
	if !strings.HasSuffix(tok.Password.Type, "#PasswordDigest") {
		t.Errorf("Password.Type = %q, want suffix #PasswordDigest", tok.Password.Type)
	}
	if tok.Nonce.Value != nonceB64 {
		t.Errorf("Nonce.Value = %q, want %q", tok.Nonce.Value, nonceB64)
	}
	if tok.Created != created {
		t.Errorf("Created = %q, want %q", tok.Created, created)
	}
}

func TestValidateAuthDigestSuccess(t *testing.T) {
	nonceRaw := []byte("randombytes123")
	nonceB64 := base64.StdEncoding.EncodeToString(nonceRaw)
	created := "2026-07-19T12:00:00Z"
	digest := computeExpectedDigest(t, nonceRaw, created, "supersecret")

	body := buildEnvelope(t, digestSecurityHeader(nonceB64, created, digest), "<tds:GetDeviceInformation/>")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	ok, reason := ValidateAuth(env, "protect", "supersecret")
	if !ok {
		t.Errorf("ValidateAuth() = false (%s), want true", reason)
	}
}

func TestValidateAuthDigestWrongPassword(t *testing.T) {
	nonceRaw := []byte("randombytes123")
	nonceB64 := base64.StdEncoding.EncodeToString(nonceRaw)
	created := "2026-07-19T12:00:00Z"
	digest := computeExpectedDigest(t, nonceRaw, created, "supersecret")

	body := buildEnvelope(t, digestSecurityHeader(nonceB64, created, digest), "<tds:GetDeviceInformation/>")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	ok, reason := ValidateAuth(env, "protect", "wrongpassword")
	if ok {
		t.Error("ValidateAuth() = true, want false for wrong password")
	}
	if reason == "" {
		t.Error("expected a non-empty reason for auth failure")
	}
}

func TestValidateAuthWrongUsername(t *testing.T) {
	nonceRaw := []byte("randombytes123")
	nonceB64 := base64.StdEncoding.EncodeToString(nonceRaw)
	created := "2026-07-19T12:00:00Z"
	digest := computeExpectedDigest(t, nonceRaw, created, "supersecret")

	body := buildEnvelope(t, digestSecurityHeader(nonceB64, created, digest), "<tds:GetDeviceInformation/>")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	ok, _ := ValidateAuth(env, "someoneelse", "supersecret")
	if ok {
		t.Error("ValidateAuth() = true, want false for wrong username")
	}
}

func TestValidateAuthPlaintextFallback(t *testing.T) {
	header := `<wsse:Security><wsse:UsernameToken>` +
		`<wsse:Username>protect</wsse:Username>` +
		`<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordText">supersecret</wsse:Password>` +
		`</wsse:UsernameToken></wsse:Security>`
	body := buildEnvelope(t, header, "<tds:GetDeviceInformation/>")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	ok, reason := ValidateAuth(env, "protect", "supersecret")
	if !ok {
		t.Errorf("ValidateAuth() = false (%s), want true for PasswordText fallback", reason)
	}
}

func TestValidateAuthNoSecurityHeader(t *testing.T) {
	body := buildEnvelope(t, "", "<tds:GetDeviceInformation/>")
	env, err := ParseEnvelope(body)
	if err != nil {
		t.Fatalf("ParseEnvelope returned error: %v", err)
	}
	ok, reason := ValidateAuth(env, "protect", "supersecret")
	if ok {
		t.Error("ValidateAuth() = true, want false when no security header present")
	}
	if reason == "" {
		t.Error("expected a non-empty reason when no security header present")
	}
}
