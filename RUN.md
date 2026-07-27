# Running & testing reolink-onvif-shim

This makes a Reolink doorbell camera adoptable by UniFi Protect as a third-party
ONVIF camera. It runs as a single native binary — no VM, no Docker.

All real values (camera IP, credentials) live only in `config.json`, which is
gitignored. Copy `config.example.json` to `config.json` and fill in your own.

Placeholders below:
- `<SHIM_HOST>` — the machine running this shim (its LAN IP)
- `<CAMERA_IP>` — the Reolink's IP (goes in `config.json` → `Target.Host`)

## 1. Configure

```sh
cp config.example.json config.json
# edit config.json: set Target.Host/Username/Password to your camera, and
# ONVIF.Username/Password to whatever you'll type into Protect.
```

Key fields:
- `ONVIF` — the credentials **Protect** authenticates to *this shim* with (you invent these).
- `Target` — the camera's real IP and login.
- `Listen` — ONVIF HTTP port (default `:8099`, a high port so no root is needed).
- `Proxy.RTSPListen` — RTSP proxy port (default `:8554`).
- `Mode` — `proxy` (Protect fetches media from this host, so we relay to the camera)
  or `direct` (Protect connects to the camera itself — simpler, but Protect ignores
  the host in the ONVIF URIs, so `proxy` is what actually works with Protect).

## 2. Run it

No root required (all ports are high):

```sh
make run          # builds and runs against config.json
```

or manually:

```sh
./reolink-onvif-shim -config config.json -log reolink-onvif-shim.log
```

Watch the log from another window:

```sh
make logs         # tail -f reolink-onvif-shim.log
make mem          # show recent MEMSTATS lines (memory tracking)
```

`debug` in the config dumps the full ONVIF request/response XML for every call
Protect makes — that's how we diagnose anything that goes wrong.

## 3. Adopt in UniFi Protect

1. UniFi Protect → **Settings → System** → enable **"Discover Third-Party Cameras"**.
2. On a **high ONVIF port** (the default), adopt via **discovery**: the shim answers
   Protect's WS-Discovery probes advertising `<SHIM_HOST>:<Listen port>`, so the camera
   appears in the third-party list — click to adopt.
   (On port **80**, you can instead use the `?` help icon → **"Try Advanced Adoption"**
   and enter `<SHIM_HOST>` directly; Advanced Adoption assumes port 80.)
3. Enter the **ONVIF** username/password from your `config.json` — *not* the camera's
   own login. Protect only ever talks to the shim.
4. You should get a live view and **continuous recording** within a few seconds.
   (Third-party cameras get continuous recording only — no motion/AI events; that's a
   Protect limitation.)

## What to send if it doesn't work

```sh
tail -100 reolink-onvif-shim.log
```

The debug log shows exactly which ONVIF actions Protect called, from what IP, on what
port, and what we returned — including whether the RTSP/snapshot proxy is being hit
(`RTSP proxy: connected` / `SNAPSHOT proxy: served`).

## Notes

- **Ports:** everything runs on high ports by default (ONVIF `:8099`, RTSP `:8554`,
  WS-Discovery `:3702`), so no root is needed. If you adopt on port 80 instead, that
  port requires root on macOS.
- **Memory tracking:** a `MEMSTATS` line logs at startup and every minute (heap,
  goroutines, GC). Flat lines over time confirm there's no leak.
## Run at boot (auto-start)

Install it as a macOS **LaunchAgent** — starts at login and is automatically
restarted if it ever exits. No root required (the shim uses high ports).

```sh
make install-service     # renders the plist, installs it, and starts the service
make service-status      # check it's running (shows state + pid)
make uninstall-service   # stop + remove it
```

- Stop any manual/tmux run first, or the agent can't bind the port (the installer
  warns if the port is already in use).
- The service runs the binary and `config.json` from this repo directory, and writes
  to `reolink-onvif-shim.log` (so `make logs` / `make mem` still work). Don't `make
  clean` or move the repo while the service is installed.
- Under the hood: `launchd` calls `launchctl bootstrap` on
  `~/Library/LaunchAgents/com.statico-alt.reolink-onvif-shim.plist`, generated from
  `launchd/*.template`. To restart after a config change: `make service-restart`.
- If you need it to run *before* login (truly headless, no auto-login), that requires a
  root LaunchDaemon instead — ask and I'll add one.
