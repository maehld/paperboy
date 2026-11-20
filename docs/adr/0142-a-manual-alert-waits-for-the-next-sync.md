# 0142: A manual alert waits for the next sync

- Status: Accepted, implemented
- Date: 2026-11-20
- Builds on: ADR 0042 (device API), 0080 (state per MAC), 0109 (pair a display), 0131 (a rule alerts the device), 0132 (alert methods per device)
- Answers: issue #350

## Context

A plan rule can alert a device ([ADR 0131](0131-a-plan-rule-alerts-the-device.md)), and the device file names the methods ([ADR 0132](0132-alert-methods-per-device.md)). An operator cannot ask one device to signal. A test of the buzzer needs a plan edit and a wait for a rule.

An alert is the only request from Paperboy that the device cannot answer. A firmware update is a comparison: Paperboy reads the reported version and offers again, so the state keeps the attempt, not the request ([ADR 0043](0043-firmware-distribution.md)). A signal changes nothing on the device. Paperboy cannot check it, repeat it or count a failure.

The device drives every request. Paperboy has no channel to a sleeping device, so a request waits until the device asks.

## Decision

1. A manual alert is a request in the device state: `pending_alert` with `at` and `until`.
2. The next sync consumes it and sets `beep` and `blink` through `alert_methods`, the same path as a rule ([ADR 0131](0131-a-plan-rule-alerts-the-device.md), [0132](0132-alert-methods-per-device.md)). The sync consumes it before it writes the state, so one write clears the request and stores the rest of the sync.
3. The request waits for two refresh intervals, at least one hour and at most one day. An expired request is dropped with a log line.
4. The battery warning drops the request, because that page carries no alert.
5. `POST /api/devices/{id}/alert` in the UI routes queues the request. The dashboard card posts it as a form and comes back to the cards.
6. A device that cannot take the request gets no button: no `alert_methods`, a `Problem` or no key. A line under the button names the reason.
7. The sync log names the source: `alert=rule`, `alert=manual` or `alert=rule+manual`.
8. A text poll never consumes the request. It reads a copy of the state, and only a sync writes that state.
9. No firmware and no protocol change. The device still knows only `beep` and `blink`.

## Consequences

- An operator signals one device without a plan. The request arrives at the next sync: up to `refresh_s` later, or at once with a button press on the device.
- A lost answer loses that signal. The operator asks again. [ADR 0131](0131-a-plan-rule-alerts-the-device.md) accepts the same loss for a rule's alert.
- A device that stays away loses the request after its lifetime, so a display does not signal for a click from yesterday.
- A silent device cannot be signalled. The card names the reason, so the operator enables a method instead of waiting.
- The state holds one request. A second click replaces the first.
- A rule and a manual request in the same sync send one answer and log `rule+manual`.

## Alternatives

- **A push request.** Paperboy has no channel to a sleeping device, and the device drives every request.
- **A reconciled request like the firmware offer.** A signal has no answer to compare. Paperboy would repeat it forever or never know whether it arrived.
- **A queue of requests.** One signal has no order and no count. A second type of one-shot request can turn the field into a list.
- **The virtual display.** It has no device and no hardware. A signal there would claim something that did not happen.
- **`alert_methods` ignored for a manual request.** A signal stays opt-in ([ADR 0132](0132-alert-methods-per-device.md)). The card names the missing methods instead.
- **A request without expiry.** A device that returns after a week would signal for an old click.
