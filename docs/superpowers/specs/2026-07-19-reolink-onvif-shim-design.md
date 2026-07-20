# Design: `reolink-onvif-shim` — a tiny Go ONVIF control-plane for UniFi Protect

**Date:** 2026-07-19
**Status:** Approved (pending spec review)

## Problem

Mom's Reolink doorbell camera (at `192.0.2.45` on her subnet, reachable from our
LAN because the two houses are linked via UniFi) needs to appear in **UniFi Protect**
as a third-party camera.

Prior attempts used [daniela-hase/onvif-server](https://github.com/daniela-hase/onvif-server)
(a Node.js ONVIF proxy) running in an Alpine Linux VM under UTM.app on macmini. It
worked for about an hour before crashing. The failure modes were:

1. **UTM VM instability** — the VM itself crashed.
2. **Memory leaks in the Node server** — it proxied *all* video bytes, and leaked
   memory in the xml2js parser, TCP proxy connections, and SOAP client until the
   process pegged CPU (GC thrashing) and hung.
3. **Complexity** — the VM existed solely to give the ONVIF server its own MAC/IP,
   which was required for ONVIF **WS-Discovery** (multicast UDP) to work.

## Key insight from research (2026-07)

UniFi Protect 5.x supports **"Advanced Adoption"**: in Protect, the `?` help icon →
"Try Advanced Adoption" lets you enter an **IP address + username + password**
directly. This performs *unicast* ONVIF adoption and **does not require multicast
WS-Discovery**.

That removes the entire reason the old setup needed a dedicated MAC/IP and therefore
a VM. We can run a plain native service on the macmini host.

Confirmed facts:
- Protect still speaks **ONVIF SOAP** to third-party cameras — there is no "paste a
  raw RTSP URL" field. So a small ONVIF responder is still required. (This is what
  onvif-server did, and it *did* work — briefly.)
- Third-party cameras get **continuous recording only** — no motion / AI events.
- The Reolink must have **both RTSP and ONVIF enabled** on the camera.

Sources:
- https://help.ui.com/hc/en-us/articles/26301104828439-Third-Party-Cameras-in-UniFi-Protect
- https://www.florian-rhomberg.net/2025/01/how-to-integrate-a-third-party-camera-into-unifi-protect/
- https://whackasstech.com/ubiquiti/unifiprotect/how-to-add-3rd-party-cameras-to-unifi-protect/

## Goals

- Get the Reolink doorbell recording continuously in UniFi Protect.
- Be **reliable**: survive for months without crashing or leaking.
- Be **simple**: a single native binary, no VM, no Docker, no runtime dependencies.
- Emit **lots of debug logging to a file** so we can see exactly what Protect does.

## Non-goals

- Motion / AI / smart detections (Protect doesn't support these for third-party cams).
- Multi-camera support (one camera; keep it simple. Config could grow later.)
- PTZ (doorbell is fixed).

## Architecture

A single static Go binary running as a **launchd LaunchDaemon on macmini**
(`198.51.100.33`). It implements just enough ONVIF for Protect to adopt and stream.
In the default ("direct") mode, **no video bytes pass through the process** — it is
purely a control plane.

```
UniFi Protect console
   │  1. Advanced Adoption: enter macmini IP + ONVIF creds
   │  2. SOAP over HTTP  ─────────────►  reolink-onvif-shim (macmini :80)
   │  3. GetStreamUri  ◄───────────────  returns rtsp://…@192.0.2.45:554/h264Preview_01_main
   │
   └─ 4. RTSP  ──────────────────────►  Reolink doorbell (192.0.2.45) directly
                                          (video never touches our process)
```

### Adoption / streaming flow

1. In Protect: Settings → System → enable "Discover Third-Party Cameras". Then
   `?` → "Try Advanced Adoption" → enter macmini IP + the ONVIF username/password
   we configured.
2. Protect POSTs SOAP requests to our HTTP server (Device + Media services).
3. We respond describing the Reolink main stream, and `GetStreamUri` returns
   `rtsp://<reolink-user>:<reolink-pass>@192.0.2.45:554/h264Preview_01_main`.
4. Protect connects **directly** to the Reolink for RTSP and records continuously.

## ONVIF surface (the minimum Protect actually calls)

**Device service:**
- `GetDeviceInformation` — manufacturer/model/firmware/serial
- `GetCapabilities` / `GetServices` — advertise Device + Media service endpoints
- `GetSystemDateAndTime` — Protect checks this; must be sane
- `GetScopes` — device type/name scopes

**Media service:**
- `GetProfiles` — one profile ("MainStream") referencing the video encoder config
- `GetVideoEncoderConfiguration` — H.264, 2560x1920, 20fps, 4096kbps (from old config)
- `GetStreamUri` — returns the Reolink RTSP URL (with embedded creds)
- `GetSnapshotUri` — returns the Reolink snapshot URL

**Security:** WS-Security `UsernameToken` with SHA1 password digest
(`Base64(SHA1(nonce + created + password))`). We validate Protect's requests against
the configured ONVIF creds and reject/log mismatches.

**WS-Discovery:** a UDP :3702 responder to `Probe` messages. Not strictly needed
given Advanced Adoption, but cheap to include and lets the camera also appear via
normal discovery.

## Components / files

Small — a few hundred lines total. Standard library only.

| File | Responsibility |
|------|----------------|
| `main.go` | Load config, set up logging, start HTTP + discovery servers, signal handling |
| `config.json` | **gitignored** — real creds live here |
| `config.example.json` | Committed template with placeholders |
| `internal/onvif/soap.go` | SOAP envelope parse/build; WS-Security digest validation |
| `internal/onvif/device.go` | Device service SOAP handlers |
| `internal/onvif/media.go` | Media service SOAP handlers |
| `internal/onvif/discovery.go` | WS-Discovery UDP responder |
| `internal/onvif/proxy.go` | Fallback TCP(RTSP)+HTTP(snapshot) proxy; dormant unless `mode: "proxy"` |
| `com.statico.reolink-onvif-shim.plist` | launchd LaunchDaemon (KeepAlive) |

Dependencies: stdlib only — `net/http`, `encoding/xml`, `crypto/sha1`,
`encoding/json`, `net`. Nothing to leak or go stale.

## Configuration (`config.json`)

```json
{
  "listen": ":80",
  "mode": "direct",
  "onvif": { "username": "protect", "password": "CHANGE_ME" },
  "device": {
    "manufacturer": "Reolink",
    "model": "Video Doorbell",
    "uuid": "12345678-1234-1234-1234-123456789abc"
  },
  "target": {
    "host": "192.0.2.45",
    "rtspPort": 554,
    "snapshotPort": 80,
    "username": "admin",
    "password": "CHANGE_ME"
  },
  "stream": {
    "rtspPath": "/h264Preview_01_main",
    "snapshotPath": "/cgi-bin/api.cgi?cmd=Snap&channel=0",
    "width": 2560, "height": 1920, "framerate": 20, "bitrate": 4096
  }
}
```

- `onvif.*` — the credentials **Protect** authenticates to *us* with (we invent these).
- `target.*` — the Reolink's real IP + login (username defaults to `admin`).
- `mode` — `"direct"` (default) or `"proxy"` (fallback).

Real secrets live only in `config.json`, which is gitignored. The committed
`config.example.json` has placeholders.

## Logging (emphasis: lots of it)

- Writes to **`/usr/local/var/log/reolink-onvif-shim.log`** *and* stderr.
- On startup: the full effective config with passwords redacted, and all listen
  addresses.
- Per SOAP request: parsed action, remote address, auth result, response status.
- With `--debug` (or `"debug": true`): the **full raw request and response XML**.
  This is how we see exactly what Protect sends — which action, which port — so we
  adapt from evidence instead of guessing.
- WS-Discovery probes and **every auth failure** are logged.

## Reliability (fixing the old failure modes)

- Native binary, no UTM → the VM-crash class is gone.
- Direct mode → no video proxying → the memory-leak class is gone.
- `launchd KeepAlive=true` → auto-restart if it ever exits.
- Every handler `recover()`s from panics and returns a SOAP fault instead of
  crashing; malformed input is logged, never fatal.
- No unbounded buffers; request bodies are size-limited.

## Testing

- **Unit tests:** WS-Security SHA1 digest computation/validation; building each SOAP
  response (`GetProfiles`, `GetStreamUri`, `GetDeviceInformation`) and asserting
  well-formed XML with expected values.
- **Smoke test:** a shell/`curl` script that POSTs a real `GetProfiles` envelope to a
  locally-running instance and asserts the profile + stream URI come back. Run this
  before ever touching Protect.
- **Manual integration:** adopt in Protect via Advanced Adoption, watch the debug
  log, confirm a live stream + continuous recording.

## Open items to verify empirically

- **Port:** Protect's advanced adoption is expected to probe ONVIF on **port 80**, so
  the HTTP listener defaults to `:80` (LaunchDaemon runs as root → can bind 80). If
  the debug log shows Protect using a different port/behavior, we adjust config.
- **Direct routing:** we assume Protect can reach `192.0.2.45`. If direct streaming
  fails, flip `mode: "proxy"` and the shim proxies the bytes as a fallback.
- **Reolink username:** assumed `admin`; correct in config if different.
