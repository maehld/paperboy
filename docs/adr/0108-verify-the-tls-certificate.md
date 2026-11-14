# 0108: Verify the TLS certificate in the firmware

- Status: Accepted, implemented (firmware not yet flashed)
- Date: 2026-11-15
- Related: ADR 0041, 0042 (firmware, device protocol), 0043 (firmware distribution), 0096 (UI password), 0097 (device key), issue #240

## Context

The firmware talks to Paperboy over HTTP in the LAN. For an `https://` Paperboy URL it switched certificate verification off (`verify_ssl: false`). A self-signed proxy then worked without a certificate in one image per hardware type (ADR 0041).

The plan is a public reverse proxy with a Let's Encrypt certificate. Without verification, HTTPS only encrypts against a passive listener. A man in the middle can read the device key. It can also replace the image the device flashes (ADR 0043), because the MD5 travels through the same unverified connection.

`http://` is different. The API has no TLS there, and an instance in a trusted LAN keeps working. `verify_ssl` only applies to `https://` requests, so the switch cannot break a LAN device.

The obstacle is the clock. Validation compares the certificate's validity dates with the system time. The firmware has no time source. The sync answer names `time` (ADR 0042), but the firmware ignored it. Without a plausible time, the first request after a power-on fails with "certificate is not yet valid".

## Decision

### 1. Verification is on, with the ESP-IDF CA bundle

The firmware drops `verify_ssl: false`, so ESPHome's default applies again. The ESP32 then verifies the chain against the ESP-IDF x509 bundle. That bundle comes from Mozilla's root store and contains the Let's Encrypt roots (ISRG Root X1 and X2). Nothing is per installation and nothing sits in the repository.

Pinned roots and `ca_certificate_path` for an internal CA are a later decision. A self-signed HTTPS proxy needs a local build with verification off. The LAN keeps using `http://`.

### 2. SNTP provides the clock, Paperboy keeps it

A `time` component with the SNTP platform asks `pool.ntp.org`. The hardware file overrides it with the `ntp_server` substitution, and `time.cloudflare.com` is the second server. The cycle waits for a plausible time (year 2024 or later) before its first request. It waits only for an `https://` URL and only for ten seconds. An unreachable NTP server must not become a device that retries a handshake it cannot win.

A wake after deep sleep still has the RTC time. ESPHome starts the SNTP poll during boot anyway, which costs one name lookup and one UDP exchange per wake. `pb_stop_sntp_if_clocked()` stops that poll when the clock is valid. The sync answer sets the clock from `time`, so drift never accumulates. Only a power-on waits for NTP.

Text displays never sleep, so they keep a clocked SNTP component. After a reboot they wait for the clock like an image display.

### 3. The page and OTA downloads inherit the verification

`http_request` is one client for every request. `ota: http_request` downloads through the same component, so nothing extra is needed. The page download already carries the device key (ADR 0097). With verification on, the key and the image are confidential and authentic on a public proxy.

### 4. A device that cannot verify says so

A failed handshake leaves the panel on its last image. When no clock arrives in time, the image firmware shows the setup screen with the reason and the MAC. The note for the next sync names the NTP problem. A text display prints `No clock for https / check NTP or use http` while it waits.

## Consequences

- An instance behind a proxy with a public certificate gets authenticity. A network capture no longer yields the device key, and nobody can replace an OTA image in transit. The enrollment gap of `register` stays open, because a MAC address is still enough (issue #241).
- A LAN instance with `http://` is untouched, including its wake time. An instance whose HTTPS proxy uses a self-signed certificate stops working after the update. It needs a public certificate or a later pinning option.
- The device now depends on NTP for its first request after a power-on. A network that blocks the pool points `ntp_server` at its router. Without any NTP the device shows the setup screen instead of an empty cycle.
- A wake costs no extra request: the RTC time stays, SNTP stops, and the sync answer corrects the clock.
- All four firmware images change: Sticky 2.8.0, E1001 2.9.0, E1003 1.1.0, lcd2004 2.3.0.
- The server needs no change, because the sync answer already named `time`.

## Considered and rejected

- **Pinned ISRG roots instead of the bundle:** they save flash and narrow the trust set. A different CA would need a new image, and the bundle already covers Let's Encrypt. Deferred, not rejected forever.
- **Trust on first use:** it has no expiry story and adds a second place for a stale certificate. The public CA model needs no state on the device.
- **Verification only on the page download:** `verify_ssl` belongs to the one HTTP client. Register and sync without verification would expose the key, which is the more valuable secret.
- **No clock and a longer retry:** every retry is a failed handshake, and the device never learns why.
