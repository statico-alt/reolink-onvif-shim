#!/bin/sh
# Manual smoke test for reolink-onvif-shim (direct mode).
#
# Builds the binary, starts it against a temp config on a local test port,
# and uses curl to exercise the real ONVIF SOAP surface:
#   1. GetSystemDateAndTime (no auth) — must succeed without credentials.
#   2. GetProfiles (WS-Security PasswordDigest auth) — must return the
#      configured encoder resolution.
#   3. GetStreamUri (WS-Security PasswordDigest auth) — must return the
#      real Reolink RTSP URL with embedded target credentials.
#
# The WS-Security digest (base64(SHA1(base64decode(nonce) + created +
# password))) is computed here with openssl so the authenticated requests
# actually authenticate against the running server.
#
# POSIX sh + curl + openssl only. Exits non-zero on any mismatch.

set -eu

fail() {
	echo "SMOKE TEST FAILED: $1" >&2
	if [ -f "$TMPDIR/server.log" ]; then
		echo "---- server log ----" >&2
		cat "$TMPDIR/server.log" >&2
		echo "---------------------" >&2
	fi
	exit 1
}

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

TMPDIR=$(mktemp -d "${TMPDIR:-/tmp}/reolink-onvif-shim-smoke.XXXXXX")
SERVER_PID=""

cleanup() {
	if [ -n "$SERVER_PID" ]; then
		kill "$SERVER_PID" >/dev/null 2>&1 || true
		wait "$SERVER_PID" 2>/dev/null || true
	fi
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

PORT=18181
HOST="127.0.0.1"
BASE_URL="http://$HOST:$PORT"
ONVIF_USER="protect"
ONVIF_PASS="testpass"
TARGET_USER="admin"
TARGET_PASS="reolinkpass"
TARGET_HOST="192.0.2.45"

echo "==> Building reolink-onvif-shim"
( cd "$REPO_ROOT" && go build -o "$TMPDIR/reolink-onvif-shim" . ) || fail "go build failed"

echo "==> Writing test config"
cat >"$TMPDIR/config.json" <<EOF
{
  "Listen": "$HOST:$PORT",
  "Debug": true,
  "Mode": "direct",
  "ONVIF": { "Username": "$ONVIF_USER", "Password": "$ONVIF_PASS" },
  "Device": {
    "Manufacturer": "Reolink",
    "Model": "Video Doorbell",
    "Firmware": "1.0.0",
    "Serial": "SMOKETEST",
    "UUID": "12345678-1234-1234-1234-123456789abc"
  },
  "Target": {
    "Host": "$TARGET_HOST",
    "RTSPPort": 554,
    "SnapshotPort": 80,
    "Username": "$TARGET_USER",
    "Password": "$TARGET_PASS"
  },
  "Stream": {
    "RTSPPath": "/h264Preview_01_main",
    "SnapshotPath": "/cgi-bin/api.cgi?cmd=Snap&channel=0",
    "Width": 2560,
    "Height": 1920,
    "Framerate": 20,
    "Bitrate": 4096
  }
}
EOF

echo "==> Starting server on $BASE_URL"
"$TMPDIR/reolink-onvif-shim" -config "$TMPDIR/config.json" -log "$TMPDIR/server.log" &
SERVER_PID=$!

# Wait for the server to accept connections (up to ~5s).
i=0
until curl -s -o /dev/null "$BASE_URL/onvif/device_service"; do
	i=$((i + 1))
	if [ "$i" -ge 50 ]; then
		fail "server did not become ready on $BASE_URL within 5s"
	fi
	sleep 0.1
done

# ---- 1. GetSystemDateAndTime: no auth required ----

echo "==> Testing GetSystemDateAndTime (no auth)"
DATETIME_BODY='<?xml version="1.0" encoding="UTF-8"?><soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><soap:Body><tds:GetSystemDateAndTime/></soap:Body></soap:Envelope>'
DATETIME_RESP=$(curl -s -X POST -H "Content-Type: application/soap+xml; charset=utf-8" --data "$DATETIME_BODY" "$BASE_URL/onvif/device_service")

case "$DATETIME_RESP" in
*GetSystemDateAndTimeResponse*) echo "    OK: got GetSystemDateAndTimeResponse" ;;
*) fail "GetSystemDateAndTime response missing GetSystemDateAndTimeResponse. Got: $DATETIME_RESP" ;;
esac

# ---- helper: compute a WS-Security PasswordDigest ----
# digest = base64(SHA1(base64decode(nonce) + created + password))
compute_digest() {
	nonce_b64="$1"
	created="$2"
	password="$3"
	{
		printf '%s' "$nonce_b64" | openssl base64 -d -A
		printf '%s%s' "$created" "$password"
	} | openssl dgst -sha1 -binary | openssl base64 -A
}

build_authed_envelope() {
	action="$1"
	nonce_b64=$(openssl rand -base64 16)
	created=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
	digest=$(compute_digest "$nonce_b64" "$created" "$ONVIF_PASS")
	cat <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
  xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
  xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
  xmlns:trt="http://www.onvif.org/ver10/media/wsdl">
  <soap:Header>
    <wsse:Security>
      <wsse:UsernameToken>
        <wsse:Username>$ONVIF_USER</wsse:Username>
        <wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">$digest</wsse:Password>
        <wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">$nonce_b64</wsse:Nonce>
        <wsu:Created>$created</wsu:Created>
      </wsse:UsernameToken>
    </wsse:Security>
  </soap:Header>
  <soap:Body>
    <trt:$action/>
  </soap:Body>
</soap:Envelope>
EOF
}

# ---- 2. GetProfiles: authenticated, check encoder resolution ----

echo "==> Testing GetProfiles (authenticated)"
PROFILES_BODY=$(build_authed_envelope "GetProfiles")
PROFILES_RESP=$(curl -s -X POST -H "Content-Type: application/soap+xml; charset=utf-8" --data "$PROFILES_BODY" "$BASE_URL/onvif/media_service")

case "$PROFILES_RESP" in
*2560*1920*) echo "    OK: got encoder resolution 2560x1920" ;;
*) fail "GetProfiles response missing expected resolution (2560/1920). Got: $PROFILES_RESP" ;;
esac

case "$PROFILES_RESP" in
*MainStream*) : ;;
*) fail "GetProfiles response missing MainStream profile token. Got: $PROFILES_RESP" ;;
esac

# ---- 3. GetStreamUri: authenticated, check real Reolink RTSP URL ----

echo "==> Testing GetStreamUri (authenticated)"
STREAMURI_BODY=$(build_authed_envelope "GetStreamUri")
STREAMURI_RESP=$(curl -s -X POST -H "Content-Type: application/soap+xml; charset=utf-8" --data "$STREAMURI_BODY" "$BASE_URL/onvif/media_service")

case "$STREAMURI_RESP" in
*"rtsp://$TARGET_USER:$TARGET_PASS@$TARGET_HOST:554/h264Preview_01_main"*)
	echo "    OK: got expected RTSP stream URI"
	;;
*)
	fail "GetStreamUri response missing expected rtsp://...$TARGET_HOST URI. Got: $STREAMURI_RESP"
	;;
esac

# ---- 4. Sanity check: bad auth is rejected ----

echo "==> Testing that a bad digest is rejected"
BAD_BODY=$(build_authed_envelope "GetProfiles" | sed 's/PasswordDigest">[^<]*</PasswordDigest">bm90dGhlcmlnaHRkaWdlc3Q=</')
BAD_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/soap+xml; charset=utf-8" --data "$BAD_BODY" "$BASE_URL/onvif/media_service")
if [ "$BAD_STATUS" = "200" ]; then
	fail "expected a bad WS-Security digest to be rejected, but got HTTP 200"
fi
echo "    OK: bad digest rejected with HTTP $BAD_STATUS"

echo "==> All smoke tests passed"
