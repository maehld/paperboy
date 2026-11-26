# 0171: A ring buffer of device log lines

- Status: Accepted, implemented
- Date: 2026-11-26
- Answers: issue #475
- Builds on: the `log` field of [ADR 0042](0042-device-protocol.md)

## Context

A sync carries a `log` field ([ADR 0042](0042-device-protocol.md)). Paperboy
writes one journal line per entry and forgets the entry afterwards. The
firmware kept sixteen lines in RAM. A deep sleep or a failed sync dropped
them, and the timing lines of `pb_time()` never left the serial console.

A device in the field has no cable. After a failure the operator sees one
warning at most, and the timing lines that show where a page change spends
its time are not visible at all.

## Decision

1. **The firmware keeps a ring in RTC memory.** It holds 24 lines of up to 80
   characters. A new line overwrites the oldest, so the ring never grows.
2. **Every line carries an ID and the device time.** The first ID after a
   power-on comes from the clock or from a random number. Paperboy drops a
   repeated delivery by ID, time and message.
3. **The timing lines join the ring.** `pb_time()` writes its line into the
   ring as well as to the console. The warning for the next wake is a warning
   line, not a buffer of its own.
4. **The sync carries the unconfirmed lines.** A failed request resends them
   with the same IDs, and a line written while the request is in flight stays
   in the ring.
5. **Paperboy keeps the newest 100 lines per device.** They live in
   `state/devices/<MAC>.json` under `log`. The device editor shows them newest
   first with time, level and message.
6. **The wire format does not change.** `log` already names `id`, `time`,
   `level`, `message` and `source`, so no protocol version is needed.

## Consequences

- The RTC block grows by about 2 KB. It replaces the 120-byte note of the
  previous firmware and fits the 8 KB of an ESP32-S3.
- A device reports several failures at the next sync, not only the last one.
- A power-on clears the ring, and a device that cannot sync for a long time
  loses its oldest lines. The newest 24 survive a deep sleep.
- The state file grows by at most 100 lines. The cap keeps it bounded.
- The timing lines reach Paperboy one sync later, because the cycle writes
  them after the request of that sync.
- Firmware before this ADR sends entries without ID and time. Paperboy still
  writes them to the journal, but cannot drop a repeated delivery, so the
  device editor can show such a line twice after a lost answer.
- All four images take a new `fw_version`, so Paperboy offers them to the
  displays.
