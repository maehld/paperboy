# 0170: A battery runtime follows the measurable series

- Status: Accepted
- Date: 2026-11-26
- Related: issue #472, [ADR 0051](0051-power-source-refresh-policy.md) (power source), [ADR 0163](0163-the-readings-in-the-device-status-and-a-reading-widget.md) (device status)

## Context

The runtime estimate from issue #207 and #236 follows the first battery scale a
device reports: remaining capacity, then percentage, then voltage. That works
for a fuel gauge, whose percentage is a coulomb count. It fails on the E1001.

The E1001 has no fuel gauge. Its firmware derives the percentage from the ADC
voltage with Seeed's E10xx curve. At the top of that curve the percentage stays
at 90-91 while the cell falls: a week of samples showed 3.99 to 3.97 V and no
readable point of charge. The estimate followed the flat percentage and never
reached the voltage. Even the voltage was hidden: the fixed 0.05 V threshold is
about three weeks away at that rate, and the linear projection to 3.0 V is about
486 days, which `maxRuntime` turned into no line at all.

Tesserae and OpenDisplay read the same ADC. Tesserae averages eight samples and
uses a measured per-model table. OpenDisplay reports the raw voltage and lets
the sender apply a curve. Neither solves the flat top; both avoid committing to
a percentage on the device.

## Decision

1. **Follow the first scale that shows a measurable discharge.** The order
   stays capacity, percentage, voltage. A scale that is present but flat no
   longer blocks the next one. A flat percentage therefore falls back to the
   voltage.
2. **Judge the voltage against its own noise.** A voltage fit of at least four
   samples needs a drop of 0.01 V and three standard errors of the fitted drop.
   A shorter series keeps 0.05 V. A fixed band cannot serve an ADC whose noise
   differs per board.
3. **Name a projection beyond a year.** `maxRuntime` stays the longest precise
   estimate. A measurable discharge beyond it shows `over a year` instead of
   nothing. The value is a bound, not a precise estimate.
4. **Average the E1001's ADC samples.** The firmware reads eight samples
   before each sync instead of one. Tesserae and Seeed's SenseCraft HMI do the
   same. One sample carries the ADC noise, and the Wi-Fi bursts sag the cell.
5. **No protocol change.** The dashboard shows `over a year`. The
   `paperboy/device-status` widget keeps the precise runtime only, because its
   `runtime_s` field cannot express a bound.

## Alternatives

- **Use Seeed's measured 101-point table.** It corrects the middle of the curve,
  but its top is flatter than the current curve. At 3.97 V it maps 90 to 91 %
  over 57 mV, so the percentage would move less, not more.
- **Add a fuel gauge.** The E1001 has no shunt or gauge. This is a hardware
  change, not a firmware fix.
- **Model the consumption.** Issue #207 rejected a model: sleep current, panel
  energy and the gauge differ per hardware.
- **Keep hiding the projection.** The operator sees no consumption for months.
- **Use a fixed 0.05 V threshold.** A quiet ADC reaches it only after weeks.

## Consequences

- An E1001 shows `over a year` after about a week of discharge. An E1003 keeps
  its percentage estimate, because its percentage moves first.
- The voltage threshold adapts to the noise of the board. A noisy series needs
  more data before it counts.
- The `Runtime` line can name a bound. A precise plugin value still requires an
  estimate below a year.
- The E1001 firmware version rises to 2.16.1 for the averaged reading.
