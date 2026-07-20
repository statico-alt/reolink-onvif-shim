// Package onvif implements a minimal ONVIF Device + Media SOAP service,
// enough for UniFi Protect to adopt a camera via unicast "Advanced
// Adoption" and stream it directly from the source camera.
package onvif

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
)

// XML namespaces used throughout the ONVIF surface we implement.
const (
	NSSOAP = "http://www.w3.org/2003/05/soap-envelope"
	NSTDS  = "http://www.onvif.org/ver10/device/wsdl"
	NSTRT  = "http://www.onvif.org/ver10/media/wsdl"
	NSTT   = "http://www.onvif.org/ver10/schema"
	NSWSSE = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	NSWSU  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	NSTER  = "http://www.onvif.org/ver10/error"

	passwordDigestSuffix = "#PasswordDigest"
	passwordTextSuffix   = "#PasswordText"
)

// wsseValue captures an element's attribute-qualified chardata, e.g.
// <wsse:Password Type="...">digest-value</wsse:Password>.
type wsseValue struct {
	Type  string `xml:"Type,attr"`
	Value string `xml:",chardata"`
}

// UsernameToken is the parsed contents of wsse:Security/wsse:UsernameToken.
type UsernameToken struct {
	Username string    `xml:"Username"`
	Password wsseValue `xml:"Password"`
	Nonce    wsseValue `xml:"Nonce"`
	Created  string    `xml:"Created"`
}

// Envelope is a lenient parse of an incoming SOAP 1.2 envelope: it captures
// the WS-Security UsernameToken (if any) and keeps the raw Body contents so
// the action name and any parameters can be extracted separately.
type Envelope struct {
	XMLName xml.Name
	Header  struct {
		Security struct {
			UsernameToken UsernameToken `xml:"UsernameToken"`
		} `xml:"Security"`
	} `xml:"Header"`
	Body struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"Body"`
}

// ParseEnvelope parses a raw SOAP request body. Element matching is by
// local name only (no namespace tag on the struct fields), so it tolerates
// SOAP 1.1 or 1.2 envelopes and varying namespace prefixes.
func ParseEnvelope(raw []byte) (*Envelope, error) {
	var env Envelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsing SOAP envelope: %w", err)
	}
	return &env, nil
}

// Action returns the local name of the first child element of soap:Body,
// e.g. "GetProfiles". This is how SOAP 1.2 requests are dispatched, since
// SOAP 1.2 has no separate SOAPAction header semantics we can rely on.
func (e *Envelope) Action() (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(e.Body.Inner))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("determining SOAP action: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local, nil
		}
	}
}

// ComputeDigest computes the WS-Security UsernameToken PasswordDigest:
// base64(SHA1(base64decode(nonce) + created + password)).
func ComputeDigest(nonceB64, created, password string) (string, error) {
	nonceRaw, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return "", fmt.Errorf("decoding nonce: %w", err)
	}
	h := sha1.New()
	h.Write(nonceRaw)
	h.Write([]byte(created))
	h.Write([]byte(password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// ValidateAuth checks the envelope's WS-Security UsernameToken against the
// configured ONVIF username/password. It accepts either a PasswordDigest
// (preferred) or a plaintext PasswordText token as a fallback. It returns
// ok=true iff the username matches and the password/digest is correct; on
// failure it returns a human-readable reason suitable for logging.
func ValidateAuth(env *Envelope, configuredUsername, configuredPassword string) (ok bool, reason string) {
	tok := env.Header.Security.UsernameToken
	if tok.Username == "" {
		return false, "no wsse:UsernameToken present in request"
	}
	if tok.Username != configuredUsername {
		return false, "username mismatch"
	}

	switch {
	case hasSuffixFold(tok.Password.Type, passwordDigestSuffix):
		if tok.Nonce.Value == "" || tok.Created == "" {
			return false, "PasswordDigest missing Nonce or Created"
		}
		expected, err := ComputeDigest(tok.Nonce.Value, tok.Created, configuredPassword)
		if err != nil {
			return false, fmt.Sprintf("invalid nonce: %v", err)
		}
		if expected != tok.Password.Value {
			return false, "digest mismatch"
		}
		return true, ""
	case hasSuffixFold(tok.Password.Type, passwordTextSuffix), tok.Password.Type == "":
		if tok.Password.Value != configuredPassword {
			return false, "plaintext password mismatch"
		}
		return true, ""
	default:
		return false, fmt.Sprintf("unsupported Password Type %q", tok.Password.Type)
	}
}

func hasSuffixFold(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return foldEqual(s[len(s)-len(suffix):], suffix)
}

func foldEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
