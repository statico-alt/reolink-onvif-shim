package onvif

import (
	"encoding/xml"
	"fmt"
	"net"
)

// hostnameOnly returns hostport with any ":port" suffix removed. If
// hostport has no port, it is returned unchanged. Used to turn an inbound
// request's Host header into a bare host for building advertised URIs.
func hostnameOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// portOf returns the port from a listen address like ":8554" or
// "0.0.0.0:8554". Returns "" if it can't be determined.
func portOf(listen string) string {
	if _, p, err := net.SplitHostPort(listen); err == nil {
		return p
	}
	return ""
}

// profileToken is the single fixed media profile this shim advertises.
const profileToken = "MainStream"

// ---- shared video source / encoder configuration building blocks ----

type videoSourceConfiguration struct {
	Token       string      `xml:"token,attr"`
	Name        string      `xml:"tt:Name"`
	SourceToken string      `xml:"tt:SourceToken"`
	Bounds      videoBounds `xml:"tt:Bounds"`
}

type videoBounds struct {
	X      int `xml:"x,attr"`
	Y      int `xml:"y,attr"`
	Width  int `xml:"width,attr"`
	Height int `xml:"height,attr"`
}

type videoEncoderConfiguration struct {
	Token       string           `xml:"token,attr"`
	Name        string           `xml:"tt:Name"`
	Encoding    string           `xml:"tt:Encoding"`
	Resolution  videoResolution  `xml:"tt:Resolution"`
	Quality     float64          `xml:"tt:Quality"`
	RateControl videoRateControl `xml:"tt:RateControl"`
}

type videoResolution struct {
	Width  int `xml:"tt:Width"`
	Height int `xml:"tt:Height"`
}

type videoRateControl struct {
	FrameRateLimit   int `xml:"tt:FrameRateLimit"`
	EncodingInterval int `xml:"tt:EncodingInterval"`
	BitrateLimit     int `xml:"tt:BitrateLimit"`
}

func (s *Service) videoSourceConfig() videoSourceConfiguration {
	return videoSourceConfiguration{
		Token:       "VideoSourceConfig",
		Name:        "VideoSourceConfig",
		SourceToken: "VideoSource",
		Bounds: videoBounds{
			X:      0,
			Y:      0,
			Width:  s.cfg.Stream.Width,
			Height: s.cfg.Stream.Height,
		},
	}
}

func (s *Service) videoEncoderConfig() videoEncoderConfiguration {
	return videoEncoderConfiguration{
		Token:    "VideoEncoderConfig",
		Name:     "VideoEncoderConfig",
		Encoding: "H264",
		Resolution: videoResolution{
			Width:  s.cfg.Stream.Width,
			Height: s.cfg.Stream.Height,
		},
		Quality: 4,
		RateControl: videoRateControl{
			FrameRateLimit:   s.cfg.Stream.Framerate,
			EncodingInterval: 1,
			BitrateLimit:     s.cfg.Stream.Bitrate,
		},
	}
}

// ---- GetProfiles / GetProfile ----

type mediaProfile struct {
	Token                     string                    `xml:"token,attr"`
	Fixed                     bool                      `xml:"fixed,attr"`
	Name                      string                    `xml:"tt:Name"`
	VideoSourceConfiguration  videoSourceConfiguration  `xml:"tt:VideoSourceConfiguration"`
	VideoEncoderConfiguration videoEncoderConfiguration `xml:"tt:VideoEncoderConfiguration"`
}

func (s *Service) mediaProfile() mediaProfile {
	return mediaProfile{
		Token:                     profileToken,
		Fixed:                     true,
		Name:                      profileToken,
		VideoSourceConfiguration:  s.videoSourceConfig(),
		VideoEncoderConfiguration: s.videoEncoderConfig(),
	}
}

type getProfilesResponse struct {
	XMLName  xml.Name       `xml:"trt:GetProfilesResponse"`
	Profiles []mediaProfile `xml:"trt:Profiles"`
}

// GetProfiles returns the single fixed "MainStream" profile.
func (s *Service) GetProfiles() ([]byte, error) {
	resp := getProfilesResponse{Profiles: []mediaProfile{s.mediaProfile()}}
	return marshalResponse(resp)
}

type getProfileResponse struct {
	XMLName xml.Name     `xml:"trt:GetProfileResponse"`
	Profile mediaProfile `xml:"trt:Profile"`
}

// GetProfile returns the single named profile, or an error if the
// requested token isn't the one profile we have.
func (s *Service) GetProfile(token string) ([]byte, error) {
	if token != "" && token != profileToken {
		return nil, fmt.Errorf("no such profile token %q", token)
	}
	resp := getProfileResponse{Profile: s.mediaProfile()}
	return marshalResponse(resp)
}

// ---- GetVideoEncoderConfiguration(s) ----

type getVideoEncoderConfigurationsResponse struct {
	XMLName        xml.Name                    `xml:"trt:GetVideoEncoderConfigurationsResponse"`
	Configurations []videoEncoderConfiguration `xml:"trt:Configurations"`
}

func (s *Service) GetVideoEncoderConfigurations() ([]byte, error) {
	resp := getVideoEncoderConfigurationsResponse{
		Configurations: []videoEncoderConfiguration{s.videoEncoderConfig()},
	}
	return marshalResponse(resp)
}

type getVideoEncoderConfigurationResponse struct {
	XMLName       xml.Name                  `xml:"trt:GetVideoEncoderConfigurationResponse"`
	Configuration videoEncoderConfiguration `xml:"trt:Configuration"`
}

func (s *Service) GetVideoEncoderConfiguration() ([]byte, error) {
	resp := getVideoEncoderConfigurationResponse{Configuration: s.videoEncoderConfig()}
	return marshalResponse(resp)
}

// ---- GetVideoSources ----

type videoSource struct {
	Token      string          `xml:"token,attr"`
	Resolution videoResolution `xml:"tt:Resolution"`
}

type getVideoSourcesResponse struct {
	XMLName      xml.Name      `xml:"trt:GetVideoSourcesResponse"`
	VideoSources []videoSource `xml:"tt:VideoSources"`
}

func (s *Service) GetVideoSources() ([]byte, error) {
	resp := getVideoSourcesResponse{
		VideoSources: []videoSource{
			{
				Token: "VideoSource",
				Resolution: videoResolution{
					Width:  s.cfg.Stream.Width,
					Height: s.cfg.Stream.Height,
				},
			},
		},
	}
	return marshalResponse(resp)
}

// ---- GetStreamUri / GetSnapshotUri ----

type mediaUri struct {
	Uri                 string `xml:"tt:Uri"`
	InvalidAfterConnect bool   `xml:"tt:InvalidAfterConnect"`
	InvalidAfterReboot  bool   `xml:"tt:InvalidAfterReboot"`
	Timeout             string `xml:"tt:Timeout"`
}

type getStreamUriResponse struct {
	XMLName  xml.Name `xml:"trt:GetStreamUriResponse"`
	MediaUri mediaUri `xml:"tt:MediaUri"`
}

// GetStreamUri returns the RTSP URL for the main stream, with credentials
// embedded.
//
// In "direct" mode it points straight at the camera, so Protect connects
// to the camera directly and no video passes through this process. In
// "proxy" mode it points back at this host's RTSP proxy port (reqHost is
// the inbound request's Host header), because Protect ignores the URI host
// and fetches media from this device's own IP — so we must serve it.
func (s *Service) GetStreamUri(reqHost string) ([]byte, error) {
	var uri string
	if s.cfg.Mode == "proxy" {
		uri = fmt.Sprintf("rtsp://%s:%s@%s:%s%s",
			s.cfg.Target.Username, s.cfg.Target.Password,
			hostnameOnly(reqHost), portOf(s.cfg.Proxy.RTSPListen), s.cfg.Stream.RTSPPath)
	} else {
		uri = fmt.Sprintf("rtsp://%s:%s@%s:%d%s",
			s.cfg.Target.Username, s.cfg.Target.Password,
			s.cfg.Target.Host, s.cfg.Target.RTSPPort, s.cfg.Stream.RTSPPath)
	}
	resp := getStreamUriResponse{
		MediaUri: mediaUri{
			Uri:                 uri,
			InvalidAfterConnect: false,
			InvalidAfterReboot:  false,
			Timeout:             "PT0S",
		},
	}
	return marshalResponse(resp)
}

type getSnapshotUriResponse struct {
	XMLName  xml.Name `xml:"trt:GetSnapshotUriResponse"`
	MediaUri mediaUri `xml:"tt:MediaUri"`
}

// GetSnapshotUri returns a snapshot URL. Reolink's snapshot endpoint takes
// credentials as query parameters rather than HTTP basic auth, so we append
// them explicitly.
//
// In "direct" mode it points straight at the camera. In "proxy" mode it
// points back at this host's HTTP snapshot proxy (served on the main Listen
// port), because Protect fetches the snapshot from this device's IP rather
// than the URI host.
func (s *Service) GetSnapshotUri(reqHost string) ([]byte, error) {
	var uri string
	if s.cfg.Mode == "proxy" {
		uri = fmt.Sprintf("http://%s:%s%s&user=%s&password=%s",
			hostnameOnly(reqHost), portOf(s.cfg.Listen), s.cfg.Stream.SnapshotPath,
			s.cfg.Target.Username, s.cfg.Target.Password)
	} else {
		uri = fmt.Sprintf("http://%s:%d%s&user=%s&password=%s",
			s.cfg.Target.Host, s.cfg.Target.SnapshotPort, s.cfg.Stream.SnapshotPath,
			s.cfg.Target.Username, s.cfg.Target.Password)
	}
	resp := getSnapshotUriResponse{
		MediaUri: mediaUri{
			Uri:                 uri,
			InvalidAfterConnect: false,
			InvalidAfterReboot:  false,
			Timeout:             "PT0S",
		},
	}
	return marshalResponse(resp)
}
