package onvif

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/statico-alt/reolink-onvif-shim/internal/config"
)

// maxRequestBytes bounds how much of a SOAP request body we'll read, so a
// malformed or hostile client can't exhaust memory.
const maxRequestBytes = 1 << 20 // 1MB

// noAuthAction is the one action ONVIF clients may call before they've
// synced their clock (and therefore before they can compute a WS-Security
// password digest).
const noAuthAction = "GetSystemDateAndTime"

// actionHandler builds the full SOAP response envelope for one action.
// env carries the parsed request (including any WS-Security header and the
// raw Body for extracting action parameters); r is available for
// request-derived data such as the Host header.
type actionHandler func(svc *Service, r *http.Request, env *Envelope) ([]byte, error)

// actionHandlers maps a SOAP action's element local name (the first child
// of soap:Body) to its handler. Dispatch is purely on this name, not on
// request path or a SOAPAction header, so Protect can hit either
// /onvif/device_service or /onvif/media_service with any action.
var actionHandlers = map[string]actionHandler{
	"GetSystemDateAndTime": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetSystemDateAndTime()
	},
	"GetDeviceInformation": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetDeviceInformation()
	},
	"GetCapabilities": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetCapabilities(r.Host)
	},
	"GetServices": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetServices(r.Host)
	},
	"GetScopes": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetScopes()
	},
	"GetProfiles": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetProfiles()
	},
	"GetProfile": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		token := extractElementText(env.Body.Inner, "ProfileToken")
		return svc.GetProfile(token)
	},
	"GetVideoEncoderConfigurations": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetVideoEncoderConfigurations()
	},
	"GetVideoEncoderConfiguration": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetVideoEncoderConfiguration()
	},
	"GetVideoSources": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetVideoSources()
	},
	"GetStreamUri": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetStreamUri(r.Host)
	},
	"GetSnapshotUri": func(svc *Service, r *http.Request, env *Envelope) ([]byte, error) {
		return svc.GetSnapshotUri(r.Host)
	},
}

// extractElementText walks bodyInner looking for the first element named
// localName and returns its text content, or "" if not found / on parse
// error. Used for simple request parameters like GetProfile's ProfileToken.
func extractElementText(bodyInner []byte, localName string) string {
	dec := xml.NewDecoder(bytes.NewReader(bodyInner))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != localName {
			continue
		}
		var val string
		if err := dec.DecodeElement(&val, &se); err != nil {
			return ""
		}
		return val
	}
}

// Server is the http.Handler that serves both ONVIF SOAP endpoints.
type Server struct {
	svc *Service
	cfg *config.Config
	log *log.Logger
}

// NewServer constructs a Server. logger must not be nil.
func NewServer(svc *Service, cfg *config.Config, logger *log.Logger) *Server {
	return &Server{svc: svc, cfg: cfg, log: logger}
}

// ServeHTTP handles a SOAP request on /onvif/device_service or
// /onvif/media_service (or, leniently, any path — dispatch is by body
// action name). It never panics: any handler panic is recovered, logged,
// and turned into a SOAP Fault.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logf("PANIC handling %s %s (remote=%s): %v", r.Method, r.URL.Path, r.RemoteAddr, rec)
			s.writeFault(w, r, http.StatusInternalServerError, FaultReceiver, fmt.Sprintf("internal error: %v", rec), "")
		}
	}()

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.logf("REQUEST %s %s remote=%s: error reading body: %v", r.Method, r.URL.Path, r.RemoteAddr, err)
		s.writeFault(w, r, http.StatusBadRequest, FaultReceiver, "could not read request body", "")
		return
	}
	if s.cfg.Debug {
		s.logf("DEBUG raw request from %s %s (remote=%s):\n%s", r.Method, r.URL.Path, r.RemoteAddr, raw)
	}

	env, err := ParseEnvelope(raw)
	if err != nil {
		s.logf("REQUEST %s %s remote=%s: malformed SOAP envelope: %v", r.Method, r.URL.Path, r.RemoteAddr, err)
		s.writeFault(w, r, http.StatusBadRequest, FaultReceiver, "malformed SOAP envelope", "")
		return
	}

	action, err := env.Action()
	if err != nil {
		s.logf("REQUEST %s %s remote=%s: could not determine SOAP action: %v", r.Method, r.URL.Path, r.RemoteAddr, err)
		s.writeFault(w, r, http.StatusBadRequest, FaultReceiver, "could not determine SOAP action", "")
		return
	}

	authResult := "no-auth"
	if action != noAuthAction {
		ok, reason := ValidateAuth(env, s.cfg.ONVIF.Username, s.cfg.ONVIF.Password)
		if !ok {
			authResult = "bad-digest"
			s.logf("REQUEST %s %s remote=%s action=%s auth=%s reason=%q", r.Method, r.URL.Path, r.RemoteAddr, action, authResult, reason)
			s.logf("AUTH FAILURE action=%s remote=%s reason=%q", action, r.RemoteAddr, reason)
			s.writeFault(w, r, http.StatusBadRequest, FaultFailedAuthentication, "authentication failed: "+reason, action)
			return
		}
		authResult = "ok"
	}

	s.logf("REQUEST %s %s remote=%s action=%s auth=%s", r.Method, r.URL.Path, r.RemoteAddr, action, authResult)

	handler, known := actionHandlers[action]
	if !known {
		s.logf("REQUEST action=%s remote=%s: unsupported action", action, r.RemoteAddr)
		s.writeFault(w, r, http.StatusBadRequest, FaultActionNotSupported, fmt.Sprintf("action %q is not supported", action), action)
		return
	}

	respBody, err := handler(s.svc, r, env)
	if err != nil {
		s.logf("REQUEST action=%s remote=%s: handler error: %v", action, r.RemoteAddr, err)
		s.writeFault(w, r, http.StatusBadRequest, FaultReceiver, err.Error(), action)
		return
	}

	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
	if s.cfg.Debug {
		s.logf("DEBUG response for action=%s:\n%s", action, respBody)
	}
	s.logf("RESPONSE action=%s status=%d", action, http.StatusOK)
}

// writeFault writes a SOAP Fault with the given subcode/reason and logs
// the outcome, including the action name (which may be empty if we
// couldn't even determine it).
func (s *Server) writeFault(w http.ResponseWriter, r *http.Request, status int, subcode, reason, action string) {
	body, err := faultResponse(subcode, reason)
	if err != nil {
		// Marshaling a fault should never fail; if it does, fall back to a
		// bare status code rather than panicking.
		s.logf("ERROR building SOAP fault response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body)
	if s.cfg.Debug {
		s.logf("DEBUG fault response:\n%s", body)
	}
	s.logf("RESPONSE action=%s status=%d fault=%s", action, status, subcode)
}

func (s *Server) logf(format string, args ...any) {
	if s.log == nil {
		return
	}
	s.log.Printf(format, args...)
}
