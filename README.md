# reolink-onvif-shim

A tiny, dependency-free Go service that makes a **Reolink** camera adoptable by
**UniFi Protect** as a third-party ONVIF camera — no VM, no Docker, no runtime deps.

Protect speaks ONVIF SOAP to the shim to discover and authenticate the camera; the
shim answers just enough of the ONVIF Device + Media services for adoption, then
relays the RTSP stream and JPEG snapshots from the real camera (Protect fetches media
from the shim's own IP, so a small proxy is required).

## Goals

A **low-memory, low-CPU ONVIF proxy that actually works with UniFi Protect.**

- **Low memory** — holds a flat ~18 MB RSS indefinitely. Video is relayed as a raw
  TCP byte stream (never decoded or buffered), so there's nothing to accumulate.
  Measured flat over 13+ hours of live traffic with no goroutine or heap growth.
- **Low CPU** — no transcoding, no parsing of the media stream; the RTSP relay is
  effectively two `io.Copy` calls. It idles near zero.
- **Reliable** — a single native binary (no VM, no Docker, no runtime deps), with
  panic recovery on every network path, HTTP timeouts, and bounded request bodies.
  It's built to run for months unattended. This replaces an earlier Node.js + VM
  setup that leaked memory and crashed within hours.
- **Actually works with Protect** — handles the quirks that make third-party adoption
  fail in practice: WS-Security digest auth, `GetSystemDateAndTime` before auth, and
  Protect fetching media from the ONVIF device's IP rather than the advertised URI.
- **Observable** — extensive debug logging plus per-minute memory stats, so you can
  see exactly what Protect does and prove there's no leak.

## Why

UniFi Protect only ingests third-party cameras via ONVIF (there's no "paste an RTSP
URL" option), and it fetches media from the ONVIF device's IP rather than the address
returned in the stream URI. This shim bridges that gap for a Reolink doorbell. It
replaces an earlier Node.js + VM setup that leaked memory and crashed.

## Quick start

```sh
cp config.example.json config.json   # then edit with your camera IP + credentials
make run                             # build + run (no root — high ports by default)
make logs                            # watch the log in another window
```

Then adopt it in UniFi Protect. See **[RUN.md](RUN.md)** for the full walkthrough,
including the adoption steps and troubleshooting.

## Features

- ONVIF Device + Media services (`GetProfiles`, `GetStreamUri`, `GetSnapshotUri`, …)
- WS-Security `UsernameToken` (SHA1 digest) authentication
- WS-Discovery responder (so Protect can auto-discover it on a high port)
- `proxy` mode: leak-free RTSP TCP relay + snapshot HTTP proxy
- Extensive debug logging + per-minute memory stats
- Standard library only; runs as a single native binary

## Configuration

All real values (camera IP, credentials) live only in `config.json`, which is
**gitignored**. `config.example.json` is a template with placeholder values.

## Development

```sh
make check      # go vet + gofmt + tests
make race       # tests under the race detector
make universal  # build a universal (Intel + Apple Silicon) macOS binary
```

Design notes: [docs/superpowers/specs/](docs/superpowers/specs/).
