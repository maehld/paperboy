# 0104: Runtime stats in the web UI

- Status: Accepted
- Date: 2026-11-13
- Related: ADR 0039 (device status), ADR 0040 (operator dashboard), ADR 0049 (plugins), ADR 0051 (power source), ADR 0060 (downloads page), ADR 0090 (download cache), ADR 0096 (UI password)

## Context

Paperboy runs as a long-lived process that starts plugin programs, renders pages, keeps PNGs and widgets in memory and answers every device sync. Operators had no view of what that costs: how much memory the service holds, whether a plugin process grew, how much time a sync spends waiting for a plugin, the state lock or PNG encoding, and whether the caches keep old images nobody asks for any more. The log names slow renders above two seconds and nothing else, so the only way to answer such a question was to attach a profiler.

The numbers are all there: Go's runtime knows heap, goroutines and GC, the operating system knows resident memory and CPU of the process and of every plugin child, the widget host knows its cache and its request times, the server knows every sync.

## Decision

1. **A page `GET /ui/stats`** next to the dashboard and the downloads page, linked in the navigation of every operator page, server-rendered without JavaScript and behind the UI password (ADR 0096). It reloads itself every ten seconds, like the dashboard, and only reads numbers: it writes no state, holds no lock while it draws and changes nothing a device sees.
2. **Paperboy itself**: start time and uptime, goroutines, `GOMAXPROCS`, heap in use and system memory, live heap and objects, stack, GC cycles with the next target and the last pause, resident memory and CPU (total time and the share of one core since the previous view).
3. **One row per plugin**: built-in or configured, running or not, failing with its retry time, program and version, PID, uptime and how often the host started it, resident memory and CPU, requests and their failures with the last error, and two timings.
4. **Every plugin request is timed in two parts**: the answer time and the wait for the plugin's request slot. The wait is the part "where something waits for something else" — a second request for the same plugin has to queue behind the first, and that queue is invisible in a total.
5. **The widget cache shows what it holds and how old it is**: entries, images, bytes, stale entries, entries past their expiry, evictions, the oldest entry, and one row per age bucket (under a minute, five minutes, an hour, a day, older) with entries, images and bytes. A cached widget is charged with its decoded image (its pixel buffer) or its lines.
6. **Paperboy's own caches are counted the same way**: rendered pages and notice pages with their count, bytes and age, and the parsed page and plan files with their count and file size.
7. **Timings are named operations with count, last, average, p50, p95 and maximum**: the sync as a whole, the configuration snapshot, the wait for the state lock, the plugin conditions, plan selection, the page render split into waiting for plugins and PNG encoding, running a menu action and writing the state file; the text poll as a whole. Percentiles come from the last 256 samples, so the page does not grow and an old outlier fades out.
8. **Per device**, one row with the sync count and its last, average, p95 and maximum duration, plus the text polls counted separately — a text display asks every second and would otherwise drown the sync numbers.
9. **CPU percentages compare with the previous view of the page.** Both the process and a plugin only expose their cumulative CPU time, so the page keeps one gauge per process between two views and says which window it measured. The first view names the total and waits for the next one. This needs no sampler thread and no history file.
10. **The operating system supplies memory and CPU where it can**: Linux reads `/proc`, Windows uses `GetProcessTimes` and `GetProcessMemoryInfo` through `syscall` (no new dependency), every other platform shows nothing rather than a guess. Virtual displays (ADR 0091) are left out of the device rows; they have no hardware and would muddy the numbers.
11. **The instrumentation lives in `internal/stats`**: a registry of named timers with a small reservoir per timer, and a gauge that turns a cumulative counter into a rate. The widget host keeps its own registry and reports its plugins and cache through `Stats`; the server wraps the hot paths with `Start`/`Observe`, which cost one clock reading plus a short append.

## Alternatives

- **A Prometheus endpoint or OpenTelemetry**: more standard, but another dependency and another thing to run; the page answers the operator's question directly, and the numbers are not meant for long-term storage.
- **A background sampler** every few seconds: would give a percentage on the first view, but adds a loop that runs even when nobody looks, and still keeps no history.
- **Counters only, without percentiles**: fewer moving parts, but an average hides the slow sync that made the pages feel sluggish.
- **Writing a statistics file** under `state/`: survives a restart, but adds write traffic and a format to migrate for numbers that are only interesting right now.
- **`/proc` only**: leaves Windows — the development platform — blind.

## Consequences

- An operator can answer "who uses the memory and the CPU", "which page is slow and why" and "does the cache still hold images nobody asks for" without a profiler.
- The hot path carries a few more clock readings and locks around map appends. The page itself reads `/proc` for each running plugin once per view.
- Numbers are per process run: a restart clears them, and the CPU share is only as good as the interval between two page views.
- The page is a diagnostic, not an alerting system. It says nothing when a plugin grows; the operator has to look.
- A new timed operation is one call at its place, so later work can widen the picture without a new page.
