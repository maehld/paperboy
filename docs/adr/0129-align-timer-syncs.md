# 0129: Align timer syncs to the interval grid

- Status: Accepted, implemented
- Date: 2026-11-17
- Builds on: ADR 0042 (device API), 0051 (power source), 0064 (device protocol v4), 0078 (the menu stays open)
- Answers: issue #301

## Context

A device with `refresh: 10m` sleeps ten minutes **after** a sync, the download and the paint. The wall-clock spacing is therefore the interval plus the work, and every device has its own phase from its last power-on, press or menu. A page that shows a clock, a departure or a schedule is composed a few seconds after the intended moment, and a plan boundary is hit late.

The server knows the interval, the power floor and the timezone. The firmware knows one number, `refresh_s`, and reports it as the wake reason `timer`.

## Decision

1. For a timer sleep the sync answer carries the **rest of the interval grid** in `refresh_s`: the next multiple of the interval after local midnight in the instance timezone (ADR 0033). Ten minutes give :00, :10 and :20.
2. The slot is the start of the sleep, not the paint. The server does not subtract an estimate of the cycle: a guess can land **before** the slot, and a clock page would then show 09:59 instead of 10:00. The device wakes a few seconds after the slot.
3. The interval is the plan rule's `refresh`, else the device's, after the floor of the power source (ADR 0051). A slot closer than the floor is skipped, and a slot is never in the past.
4. A plan boundary or the page timeout already caps the sleep. That cap wins and is not stretched onto the grid; the next sync aligns again. So a cycle with a cap reports the cap, not the interval.
5. Text displays poll and never sleep. Their `refresh_s` stays the poll interval (ADR 0045).
6. Both power sources align: a battery device deep-sleeps the rest, a mains device stays awake and waits it.
7. The alignment is implicit for every device. There is no switch, and no device or plan field.
8. All devices share the slot of their interval. There is no per-MAC offset, because the point is the round clock time and a home instance has few devices.

`refresh_s` therefore means **the sleep until the next sync**, not the plan interval. That is what the value already was when a plan boundary capped it.

## Consequences

- The dashboard's refresh, `last_refresh` and the `late` check use the sleep, so they show the rest of the interval and change from cycle to cycle. The plan view names the plan's interval.
- A failed sync sleeps the last received `refresh_s`. A short rest therefore retries sooner than the interval during an outage. The floor bounds it, and a plan boundary had the same effect before.
- Intervals that divide the hour land on round clock times. Seven minutes or 50 minutes walk through the clock; the grid is documented, not refused.
- A DST change keeps the interval and moves the wall clock with it. A zone with a 30-minute shift moves the slots by that shift.
- No firmware change and no protocol change: the shipped firmware already sleeps `refresh_s`.

## Alternatives

- **The firmware aligns** (`pb_sleep_seconds` rounds against the device clock). It measures the cycle, so it is exact to the second. But it only sees the capped `refresh_s`, so it would round a boundary cap into the wrong grid, and it has no timezone for local clock times. It also needs a release and a flash of every hardware type, and older devices would keep drifting.
- **A one-shot `wake` with the rest.** It keeps `refresh_s` as the interval, which is nicer for the dashboard and for the retry after a failed sync, but it adds a second number for one sleep and a second meaning of `wake` next to the menu deadline and the action result. The value of hitting a minute does not pay for that.
- **`wake.at` with an absolute moment** would let the firmware measure the cycle and land on the slot to the second. It needs a protocol and firmware change and is left for later; the node above explains why the current step does not need it.
- **A per-MAC seconds offset** spreads the load of many devices and keeps the minute. A home instance has no such load.
