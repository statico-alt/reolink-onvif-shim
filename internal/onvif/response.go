package onvif

import (
	"encoding/xml"
	"fmt"
)

// envelopeTemplate wraps a marshaled response (or fault) element inside a
// SOAP 1.2 envelope. Namespace prefixes are declared on the envelope for
// readability in logs; the actual response element carries its own
// namespace (via its XMLName), which is what a spec-compliant client uses
// to identify it.
const envelopeTemplate = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<soap:Envelope xmlns:soap="` + NSSOAP + `" xmlns:tds="` + NSTDS + `" xmlns:trt="` + NSTRT + `" xmlns:tt="` + NSTT + `" xmlns:wsse="` + NSWSSE + `" xmlns:wsu="` + NSWSU + `" xmlns:ter="` + NSTER + `">` +
	`<soap:Body>%s</soap:Body>` +
	`</soap:Envelope>`

// marshalResponse marshals v (a struct representing a single ONVIF
// response element, e.g. GetDeviceInformationResponse) and wraps it in a
// SOAP envelope, returning the full response body bytes.
func marshalResponse(v any) ([]byte, error) {
	inner, err := xml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling response: %w", err)
	}
	return []byte(fmt.Sprintf(envelopeTemplate, inner)), nil
}

// soapFaultSubcode values used by faultResponse.
const (
	FaultActionNotSupported   = "ter:ActionNotSupported"
	FaultFailedAuthentication = "wsse:FailedAuthentication"
	FaultReceiver             = "soap:Receiver"
)

// soapFault represents a minimal SOAP 1.2 Fault body.
type soapFault struct {
	XMLName xml.Name        `xml:"http://www.w3.org/2003/05/soap-envelope Fault"`
	Code    soapFaultCode   `xml:"Code"`
	Reason  soapFaultReason `xml:"Reason"`
}

type soapFaultCode struct {
	Value   string           `xml:"Value"`
	Subcode soapFaultSubcode `xml:"Subcode"`
}

type soapFaultSubcode struct {
	Value string `xml:"Value"`
}

type soapFaultReason struct {
	Text string `xml:"Text"`
}

// faultResponse builds a SOAP 1.2 Fault envelope body with the given
// subcode (e.g. FaultActionNotSupported) and human-readable reason text.
func faultResponse(subcode, reason string) ([]byte, error) {
	f := soapFault{
		Code: soapFaultCode{
			Value:   "soap:Sender",
			Subcode: soapFaultSubcode{Value: subcode},
		},
		Reason: soapFaultReason{Text: reason},
	}
	return marshalResponse(f)
}
