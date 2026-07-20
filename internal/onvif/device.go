package onvif

import (
	"encoding/xml"
	"time"
)

// ---- GetSystemDateAndTime ----

type getSystemDateAndTimeResponse struct {
	XMLName           xml.Name          `xml:"tds:GetSystemDateAndTimeResponse"`
	SystemDateAndTime systemDateAndTime `xml:"tt:SystemDateAndTime"`
}

type systemDateAndTime struct {
	DateTimeType    string        `xml:"tt:DateTimeType"`
	DaylightSavings bool          `xml:"tt:DaylightSavings"`
	UTCDateTime     onvifDateTime `xml:"tt:UTCDateTime"`
}

type onvifDateTime struct {
	Time onvifTime `xml:"tt:Time"`
	Date onvifDate `xml:"tt:Date"`
}

type onvifTime struct {
	Hour   int `xml:"tt:Hour"`
	Minute int `xml:"tt:Minute"`
	Second int `xml:"tt:Second"`
}

type onvifDate struct {
	Year  int `xml:"tt:Year"`
	Month int `xml:"tt:Month"`
	Day   int `xml:"tt:Day"`
}

// GetSystemDateAndTime returns the current UTC time. Per ONVIF convention
// (and this brief), it must be answerable without authentication, since
// clients call it first to sync clocks before they can compute a password
// digest.
func (s *Service) GetSystemDateAndTime() ([]byte, error) {
	now := time.Now().UTC()
	resp := getSystemDateAndTimeResponse{
		SystemDateAndTime: systemDateAndTime{
			DateTimeType:    "Manual",
			DaylightSavings: false,
			UTCDateTime: onvifDateTime{
				Time: onvifTime{Hour: now.Hour(), Minute: now.Minute(), Second: now.Second()},
				Date: onvifDate{Year: now.Year(), Month: int(now.Month()), Day: now.Day()},
			},
		},
	}
	return marshalResponse(resp)
}

// ---- GetDeviceInformation ----

type getDeviceInformationResponse struct {
	XMLName         xml.Name `xml:"tds:GetDeviceInformationResponse"`
	Manufacturer    string   `xml:"tds:Manufacturer"`
	Model           string   `xml:"tds:Model"`
	FirmwareVersion string   `xml:"tds:FirmwareVersion"`
	SerialNumber    string   `xml:"tds:SerialNumber"`
	HardwareId      string   `xml:"tds:HardwareId"`
}

func (s *Service) GetDeviceInformation() ([]byte, error) {
	firmware := s.cfg.Device.Firmware
	if firmware == "" {
		firmware = "1.0.0"
	}
	serial := s.cfg.Device.Serial
	if serial == "" {
		serial = s.cfg.Device.UUID
	}
	resp := getDeviceInformationResponse{
		Manufacturer:    s.cfg.Device.Manufacturer,
		Model:           s.cfg.Device.Model,
		FirmwareVersion: firmware,
		SerialNumber:    serial,
		HardwareId:      s.cfg.Device.Model,
	}
	return marshalResponse(resp)
}

// ---- GetCapabilities ----

type getCapabilitiesResponse struct {
	XMLName      xml.Name     `xml:"tds:GetCapabilitiesResponse"`
	Capabilities capabilities `xml:"tt:Capabilities"`
}

type capabilities struct {
	Device deviceCapabilities `xml:"tt:Device"`
	Media  mediaCapabilities  `xml:"tt:Media"`
}

type deviceCapabilities struct {
	XAddr string `xml:"tt:XAddr"`
}

type mediaCapabilities struct {
	XAddr string `xml:"tt:XAddr"`
}

// GetCapabilities advertises where our Device and Media services live.
// host is derived from the inbound request's Host header so the advertised
// XAddr matches how the client reached us.
func (s *Service) GetCapabilities(host string) ([]byte, error) {
	resp := getCapabilitiesResponse{
		Capabilities: capabilities{
			Device: deviceCapabilities{XAddr: baseURL(host, "/onvif/device_service")},
			Media:  mediaCapabilities{XAddr: baseURL(host, "/onvif/media_service")},
		},
	}
	return marshalResponse(resp)
}

// ---- GetServices ----

type getServicesResponse struct {
	XMLName  xml.Name       `xml:"tds:GetServicesResponse"`
	Services []onvifService `xml:"tds:Service"`
}

type onvifService struct {
	Namespace string       `xml:"tds:Namespace"`
	XAddr     string       `xml:"tds:XAddr"`
	Version   onvifVersion `xml:"tds:Version"`
}

type onvifVersion struct {
	Major int `xml:"tt:Major"`
	Minor int `xml:"tt:Minor"`
}

// GetServices lists the Device and Media services we implement.
// IncludeCapability is accepted but ignored; we always omit the detailed
// per-service capability payload, which is optional per the ONVIF schema.
func (s *Service) GetServices(host string) ([]byte, error) {
	resp := getServicesResponse{
		Services: []onvifService{
			{
				Namespace: NSTDS,
				XAddr:     baseURL(host, "/onvif/device_service"),
				Version:   onvifVersion{Major: 2, Minor: 40},
			},
			{
				Namespace: NSTRT,
				XAddr:     baseURL(host, "/onvif/media_service"),
				Version:   onvifVersion{Major: 2, Minor: 40},
			},
		},
	}
	return marshalResponse(resp)
}

// ---- GetScopes ----

type getScopesResponse struct {
	XMLName xml.Name     `xml:"tds:GetScopesResponse"`
	Scopes  []onvifScope `xml:"tds:Scopes"`
}

type onvifScope struct {
	ScopeDef  string `xml:"tt:ScopeDef"`
	ScopeItem string `xml:"tt:ScopeItem"`
}

func (s *Service) GetScopes() ([]byte, error) {
	model := s.cfg.Device.Model
	resp := getScopesResponse{
		Scopes: []onvifScope{
			{ScopeDef: "Fixed", ScopeItem: "onvif://www.onvif.org/type/video_encoder"},
			{ScopeDef: "Fixed", ScopeItem: "onvif://www.onvif.org/name/" + model},
			{ScopeDef: "Fixed", ScopeItem: "onvif://www.onvif.org/hardware/" + model},
			{ScopeDef: "Fixed", ScopeItem: "onvif://www.onvif.org/location/"},
		},
	}
	return marshalResponse(resp)
}
