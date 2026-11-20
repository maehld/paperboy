# 0134: Readings from the device

- Status: Accepted, implemented
- Date: 2026-11-20
- Builds on: ADR 0042 (device API), 0080 (device state per MAC), 0121 (segmented battery gauge)
- Issue: #326

## Context

Every reTerminal E series device and the Sticky carry a Sensirion SHT4x
temperature and humidity sensor. The firmware ignored it, so a page could not
show the room it hangs in. The weather plugin is the only source of temperature
and humidity, and it reports the outside.

The sensor is part of the hardware, so the device has to read it and report it
to Paperboy. The device sleeps between cycles and the device API has no data
endpoint by design (ADR 0042), so the value has to travel with the sync.

The first draft of the issue sent `temperature` and `humidity` as top-level
fields and put a `climate` object into the plugin status. That makes every
later sensor another field here and another object there, next to `battery` and
`events`. Buttons already travel as a list of typed events, and a sensor is not
a special case of the battery.

## Decision

1. **The sync body carries a generic `readings` list.** Each entry is
   `{name, value}`. The device sends a bare number in the catalog's unit. The
   list travels next to `events`, `battery` and `log`, and a later sensor needs
   no new field.
2. **Paperboy owns the catalog** (`internal/reading`): name, display title,
   unit, precision and plausible range. The catalog is the contract between
   firmware and server, so a wrong firmware constant cannot label a value
   falsely, and a page formats the value in its own language later.
3. **The firmware reports room readings, not sensor readings.** The SHT4x on
   the Sticky (`0x44`, sensor bus GPIO0/1), the E1001 (new bus GPIO19/20, the
   console moves to UART0) and the E1003 (`0x44`, the GT911 bus) takes one
   high-repeatability measurement per wake, before the sync. A failed read
   leaves the reading out. `pb_add_reading` replaces an earlier value of the
   same name, so a retried sync sends one entry per reading.
4. **Paperboy stores the last value per device and MAC** with the time it was
   taken. There is no history. A sync that brings no value keeps the old one,
   and the stored time makes a stale value visible.
5. **Paperboy discards instead of failing.** An unknown name, a missing value
   and a value outside the catalog range are dropped, with one log line. A
   sensor fault must never cost the device its page, unlike `battery.volts`,
   which is required.
6. **The dashboard shows the values** as a `Readings` row in the card's
   Details: `21.5 °C · 45 %`, with the catalog names as the tooltip. A reading
   the last sync did not bring shows its age. A device without readings keeps
   no row.
7. **The plugin protocol does not change yet.** No widget and no value reads
   the readings in this step, so `DeviceStatus` stays as it is. The plugin
   status and the page values `paperboy/temperature` and `paperboy/humidity`
   follow with the widget that shows them.
8. **Calibration comes later.** The sensor sits next to the panel and the
   battery and reads slightly warm. An offset belongs to the device file and
   applies to the stored value, so the dashboard and a later widget show the
   same number. This ADR fixes the place, not the offset.

## Alternatives

- **Top-level `temperature` and `humidity` fields:** what the issue proposed.
  Compact today, but every further sensor extends the sync body and the state
  again, and the second word for a measured value becomes a pattern.
- **A `climate` object in the plugin status:** a plugin object per physical
  area instead of one channel for measured values.
- **Units on the wire:** the firmware would label its own value, so a wrong
  constant reads as a plausible number. It also moves language and number
  formatting into the firmware.
- **A reading that fails the sync:** a disconnected sensor would cost the page
  of that wake, and the device would retry the same broken read three times.
- **Read the sensor over the device API or a plugin:** the device sleeps
  between cycles, and the device API has no data endpoint. A plugin must not
  know devices.
- **Separate names such as `indoor-temperature`:** the value is the same
  quantity as `weather/temperature`, and two words for one thing are a
  glossary bug. The plugin prefix keeps the room apart from the outside.
- **Leave the sensor to Home Assistant:** then Paperboy cannot show the room it
  hangs in.

## Consequences

- The firmware needs one I2C read per wake, about 10 ms, and three new versions
  (Sticky 2.13.0, E1001 2.14.0, E1003 1.6.0).
- The E1001 loses its USB console, because the sensor shares GPIO19/20 with the
  USB pair. The deep sleep current of the new bus is not measured yet.
- The state file gains a map per device. A lost `state/devices/` also loses the
  readings until the next sync.
- The dashboard shows the values without a widget on the page. A page that
  shows its own room follows with the plugin status.
- The catalog rejects a reading outside -40…125 °C or 0…100 %. A firmware bug
  there costs the value of that wake, not the page.
