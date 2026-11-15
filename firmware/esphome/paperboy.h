// Helpers for the Paperboy firmware (ADR 0041), included by common.yaml. All
// knowledge of the device API (ADR 0042) that is easier in C++ than in YAML
// lives here: the sync body, the event queue and the image bookkeeping.
#pragma once

#include "esphome.h"
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <string>
#include <utility>
#include <vector>

#include <esp_heap_caps.h>
#include <esp_sntp.h>
#include <esp_system.h>
#include <sys/time.h>

// Kept in RTC memory: survives deep sleep, cleared on power-on, and costs no
// flash writes per wake (unlike globals with restore_value).
RTC_DATA_ATTR static char pb_shown_hash[32];  // hash of the image on the panel
RTC_DATA_ATTR static int pb_refresh_s;         // last refresh_s from Paperboy
RTC_DATA_ATTR static char pb_note[120];        // one warning for the next sync, e.g. a failed download

// Event queue and identities (ADR 0064). They live in RTC memory so that a
// failed sync keeps its events and resends them with the same IDs.
RTC_DATA_ATTR static uint32_t pb_event_seq;      // last assigned event ID
RTC_DATA_ATTR static uint32_t pb_event_sent;     // highest event ID the last request carried
RTC_DATA_ATTR static char pb_event_queue[1200];  // queued events as JSON objects, comma separated
RTC_DATA_ATTR static char pb_next_reason[16];    // reason of the next wake: menu or result
RTC_DATA_ATTR static uint32_t pb_next_s;         // seconds until that one-shot wake

// Guards the RTC layout. RTC memory survives a reboot, so after a firmware
// update the new variables above would read whatever the old image left there:
// a queue without a terminator turns every sync into invalid JSON. The marker
// is declared last, so all older variables keep their offsets; bump it whenever
// this block changes.
RTC_DATA_ATTR static uint32_t pb_rtc_magic;
constexpr uint32_t PB_RTC_MAGIC = 0x50423032;  // "PB02"

// Zeroes the RTC state once per layout. Safe to call on every boot.
inline void pb_init_rtc() {
  if (pb_rtc_magic == PB_RTC_MAGIC) {
    return;
  }
  pb_rtc_magic = PB_RTC_MAGIC;
  pb_event_seq = 0;
  pb_event_sent = 0;
  pb_event_queue[0] = 0;
  pb_next_reason[0] = 0;
  pb_next_s = 0;
  pb_note[0] = 0;
  // The panel shows an image of the previous firmware: repaint it.
  pb_shown_hash[0] = 0;
}

// The clock that TLS needs (ADR 0108). A handshake compares the certificate's
// validity dates with the system time, and the device has no RTC battery: after
// a power-on it waits for SNTP, a wake still has the RTC time of the deep
// sleep, and every sync answer corrects it. Anything before 2024 counts as
// unset, so a device that boots without a network reports "no time" instead of
// 1970.
constexpr time_t PB_CLOCK_MIN = 1704067200;  // 2024-01-01T00:00:00Z

inline bool pb_clock_valid() {
  struct timeval tv;
  gettimeofday(&tv, nullptr);
  return tv.tv_sec >= PB_CLOCK_MIN;
}

// Takes Paperboy's time (the sync answer names it in seconds).
inline void pb_set_clock(int64_t epoch) {
  if (epoch < PB_CLOCK_MIN) {
    return;
  }
  struct timeval tv;
  tv.tv_sec = static_cast<time_t>(epoch);
  tv.tv_usec = 0;
  settimeofday(&tv, nullptr);
}

// Only an https Paperboy URL needs the clock before its first request; an
// instance in the LAN keeps using http and never waits for SNTP.
inline bool pb_needs_clock(const std::string &base) { return base.rfind("https://", 0) == 0; }

inline bool pb_clock_ready(const std::string &base) {
  return !pb_needs_clock(base) || pb_clock_valid();
}

// Deep sleep keeps the time, so a wake does not need SNTP. Stopping the poll
// that the time component started during boot saves one name lookup and one
// UDP exchange per wake; the sync answer corrects the drift.
inline void pb_stop_sntp_if_clocked() {
  if (pb_clock_valid() && esp_sntp_enabled()) {
    esp_sntp_stop();
  }
}

// Cycle timing. millis() counts from the last boot, and a button press wakes
// the chip from deep sleep, so the numbers start at the press. Every line
// carries the milliseconds since boot and the delta to the line before, which
// shows how a page change spends its time on Wi-Fi, sync, download and paint.
static uint32_t pb_time_last = 0;

inline void pb_time(const char *step) {
  uint32_t now = esphome::millis();
  ESP_LOGI("paperboy", "timing %s: %lu ms (+%lu)", step, (unsigned long) now,
           (unsigned long) (pb_time_last == 0 ? 0 : now - pb_time_last));
  pb_time_last = now;
}

// True while an image was downloaded and still has to be reported as shown,
// after the panel painted it (ADR 0064).
static bool pb_paint_pending = false;

// True while the power-on splash shows the mark instead of a page: the display
// lambda draws it then, and no page may be painted meanwhile (ADR 0095).
static bool pb_splash = false;

// True while the device installs a firmware offer: the display lambda draws the
// update screen then, because the panel stays quiet through the download and
// the flash (ADR 0043).
static bool pb_installing = false;

// Battery reading of this wake; NAN = not measured. Set by the device file's
// read_battery script before any decision uses it.
static float pb_bat_volts = NAN;
static float pb_bat_percent = NAN;
static float pb_bat_mah = NAN;
static float pb_bat_ma = NAN;

// Charger state from the BQ25616 STAT pin: 1 while charging, 0 idle or full,
// -1 when the hardware has no such pin. Set by the device file.
static int pb_charging = -1;

// Power source for the next sync (ADR 0051): "battery", "mains" or empty when
// the hardware cannot tell. Set by the device file.
static std::string pb_power;

// Events since the last sync are kept in RTC memory; log entries for the next
// sync stay in RAM and are resent with the events.
static std::vector<std::string> pb_logs;

inline std::string pb_json_string(const std::string &s) {
  std::string out = "\"";
  for (char c : s) {
    switch (c) {
      case '"':
        out += "\\\"";
        break;
      case '\\':
        out += "\\\\";
        break;
      case '\n':
        out += "\\n";
        break;
      default:
        if ((unsigned char) c < 0x20) {
          out += ' ';
        } else {
          out += c;
        }
    }
  }
  return out + "\"";
}

// Remembers a warning across deep sleep, so the next sync can report it.
inline void pb_note_for_next_wake(const std::string &message) {
  std::strncpy(pb_note, message.c_str(), sizeof(pb_note) - 1);
  pb_note[sizeof(pb_note) - 1] = 0;
}

inline bool pb_queue_event(const std::string &json) {
  size_t len = std::strlen(pb_event_queue);
  if (len + json.size() + 2 >= sizeof(pb_event_queue)) {
    return false;
  }
  if (len) {
    pb_event_queue[len++] = ',';
  }
  std::memcpy(pb_event_queue + len, json.c_str(), json.size() + 1);
  return true;
}

// pb_button queues one button event. hold_ms is the measured press duration;
// the server decides from it whether the press was short or long (ADR 0067).
inline void pb_button(const std::string &role, uint32_t hold_ms) {
  pb_event_seq++;
  if (!pb_queue_event("{\"id\":" + std::to_string(pb_event_seq) + ",\"type\":\"button\",\"button\":" +
                      pb_json_string(role) + ",\"hold_ms\":" + std::to_string(hold_ms) + "}")) {
    pb_note_for_next_wake("event queue full");
  }
}

// The quarter turn the device asks for on top of the orientation the firmware
// was built for (ADR 0102). It is re-applied on every paint, because the
// display component starts from the compiled rotation after each boot.
static int pb_rotation = 0;

// pb_set_rotation takes a rotation from the sync answer; anything that is not
// a quarter turn falls back to 0 instead of painting into an unknown space.
inline void pb_set_rotation(int deg) {
  deg = ((deg % 360) + 360) % 360;
  switch (deg) {
    case 0:
    case 90:
    case 180:
    case 270:
      pb_rotation = deg;
      break;
    default:
      pb_rotation = 0;
  }
}

// pb_rotate_display applies the relative rotation to a display whose compiled
// orientation is built_deg. The generic DisplayBuffer rotation maps both the
// drawing coordinates and get_width()/get_height(), so the QOI, the splash and
// the setup screens follow it.
template<typename D> inline void pb_rotate_display(D &it, int built_deg) {
  switch ((built_deg + pb_rotation) % 360) {
    case 90:
      it.set_rotation(esphome::display::DISPLAY_ROTATION_90_DEGREES);
      break;
    case 180:
      it.set_rotation(esphome::display::DISPLAY_ROTATION_180_DEGREES);
      break;
    case 270:
      it.set_rotation(esphome::display::DISPLAY_ROTATION_270_DEGREES);
      break;
    default:
      it.set_rotation(esphome::display::DISPLAY_ROTATION_0_DEGREES);
  }
}

// One contact is one event. The panel reports a position continuously while a
// finger rests on it, so a tap is the first report of a contact and every
// further report is dropped until the finger lifts (on_release). The old
// 800 ms gap also swallowed a deliberate second tap (issue #136). A contact
// that reports nothing for ten seconds counts as lifted, so a missed release
// cannot mute the panel.
//
// The touch transform was measured in the compiled orientation; the event
// carries page coordinates, so the relative rotation turns it the same way the
// display turns the pixels (ADR 0102). width/height are the dimensions of the
// compiled space (the panel's native size for the shipped hardware).
static bool pb_touch_down = false;
static uint32_t pb_touch_last_ms = 0;

inline void pb_touch(int x, int y, int width, int height) {
  uint32_t now_ms = esphome::millis();
  if (pb_touch_down && now_ms - pb_touch_last_ms < 10000) {
    pb_touch_last_ms = now_ms;
    return;
  }
  pb_touch_down = true;
  pb_touch_last_ms = now_ms;
  int page_x = x;
  int page_y = y;
  switch (pb_rotation) {
    case 90:
      page_x = y;
      page_y = width - 1 - x;
      break;
    case 180:
      page_x = width - 1 - x;
      page_y = height - 1 - y;
      break;
    case 270:
      page_x = height - 1 - y;
      page_y = x;
      break;
  }
  pb_event_seq++;
  if (!pb_queue_event(esphome::str_sprintf("{\"id\":%lu,\"type\":\"touch\",\"x\":%d,\"y\":%d}",
                                           (unsigned long) pb_event_seq, page_x, page_y))) {
    pb_note_for_next_wake("event queue full");
  }
}

inline void pb_touch_release() { pb_touch_down = false; }

inline void pb_log(const std::string &level, const std::string &message) {
  if (pb_logs.size() < 16) {
    pb_logs.push_back("{\"level\":" + pb_json_string(level) + ",\"message\":" + pb_json_string(message) +
                      ",\"source\":\"firmware\"}");
  }
}

inline bool pb_has_events() { return pb_event_queue[0] != 0; }

// pb_sync_body builds the sync body from the queued events; the queue is only
// dropped by pb_sync_done after Paperboy accepted it (ADR 0064), so a failed
// request resends the same events with the same IDs.
inline std::string pb_sync_body(const std::string &firmware, const std::string &hardware, const std::string &reason,
                                int rssi) {
  if (pb_note[0]) {
    pb_log("warn", pb_note);
    pb_note[0] = 0;
  }
  std::string b = "{\"firmware\":" + pb_json_string(firmware) + ",\"hardware\":" + pb_json_string(hardware) +
                  ",\"reason\":" + pb_json_string(reason) + ",\"shown\":" + pb_json_string(pb_shown_hash);
  b += ",\"events\":[";
  b += pb_event_queue;
  b += "]";
  // Remember what this request carries: a press that arrives while it is in
  // flight gets a higher ID and must survive the answer (issue #136).
  pb_event_sent = pb_event_seq;
  if (std::isfinite(pb_bat_volts) && pb_bat_volts > 0) {
    b += esphome::str_sprintf(",\"battery\":{\"volts\":%.3f", pb_bat_volts);
    if (std::isfinite(pb_bat_percent))
      b += esphome::str_sprintf(",\"percent\":%.0f", pb_bat_percent);
    if (std::isfinite(pb_bat_mah))
      b += esphome::str_sprintf(",\"mah\":%.1f", pb_bat_mah);
    if (std::isfinite(pb_bat_ma))
      b += esphome::str_sprintf(",\"ma\":%.1f", pb_bat_ma);
    if (pb_charging >= 0)
      b += std::string(",\"charging\":") + (pb_charging ? "true" : "false");
    b += "}";
  }
  b += esphome::str_sprintf(",\"rssi\":%d", rssi);
  if (!pb_power.empty())
    b += ",\"power\":" + pb_json_string(pb_power);
  b += ",\"log\":[";
  for (size_t i = 0; i < pb_logs.size(); i++) {
    b += (i ? "," : "") + pb_logs[i];
  }
  b += "]}";
  return b;
}

// pb_events_drop_sent removes exactly the events the last request carried.
// The event queue stays a flat list of objects with an `id` and no nested
// braces, so the objects can be split at their closing brace. Events queued
// while the request was in flight have a higher ID and stay, which is what
// used to lose a press (issue #136).
inline void pb_events_drop_sent() {
  if (pb_event_queue[0] == 0) {
    return;
  }
  static char kept[sizeof(pb_event_queue)];
  size_t k = 0;
  const char *p = pb_event_queue;
  while (*p) {
    const char *end = std::strchr(p, '}');
    if (end == nullptr) {
      break;
    }
    uint32_t id = 0;
    if (const char *field = std::strstr(p, "\"id\":"); field != nullptr && field < end) {
      id = (uint32_t) std::strtoul(field + 5, nullptr, 10);
    }
    const size_t len = (size_t) (end - p) + 1;
    if (id > pb_event_sent) {
      if (k > 0) {
        kept[k++] = ',';
      }
      std::memcpy(kept + k, p, len);
      k += len;
    }
    p = end + 1;
    if (*p == ',') {
      p++;
    }
  }
  kept[k] = 0;
  std::memcpy(pb_event_queue, kept, k + 1);
}

// pb_sync_done drops what Paperboy confirmed: the events of that request and
// the logs. Anything queued after the body was built stays in the queue.
inline void pb_sync_done() {
  pb_events_drop_sent();
  pb_logs.clear();
}

// The next wake is a one-shot with its own reason and delay (ADR 0064).
inline void pb_set_wake(const std::string &reason, uint32_t seconds) {
  std::strncpy(pb_next_reason, reason.c_str(), sizeof(pb_next_reason) - 1);
  pb_next_reason[sizeof(pb_next_reason) - 1] = 0;
  pb_next_s = seconds;
}

// Seconds until the next sleep; the one-shot wake wins over refresh_s.
inline uint32_t pb_sleep_seconds() {
  if (pb_next_s > 0) {
    return pb_next_s;
  }
  return pb_refresh_s > 0 ? (uint32_t) pb_refresh_s : 600;
}

// Takes the reason of this wake: a one-shot reason if the last sync asked for
// one, otherwise the caller's default.
inline std::string pb_take_reason(const std::string &fallback) {
  if (!pb_next_reason[0]) {
    return fallback;
  }
  std::string reason = pb_next_reason;
  pb_next_reason[0] = 0;
  pb_next_s = 0;
  return reason;
}

inline bool pb_unchanged(const std::string &hash) { return !hash.empty() && hash == pb_shown_hash; }

inline void pb_remember(const std::string &hash) {
  std::strncpy(pb_shown_hash, hash.c_str(), sizeof(pb_shown_hash) - 1);
  pb_shown_hash[sizeof(pb_shown_hash) - 1] = 0;
}

// The reset reason of this boot. An abnormal reset (panic, watchdog, brownout)
// reaches the cycle as "powercycle", draws the splash and shows no error, so
// this is the only trace of it: the reason and the free memory go into the next
// sync's log, where Paperboy writes them to the device journal.
inline const char *pb_reset_reason() {
  switch (esp_reset_reason()) {
    case ESP_RST_POWERON:
      return "power on";
    case ESP_RST_EXT:
      return "external reset";
    case ESP_RST_SW:
      return "software restart";
    case ESP_RST_PANIC:
      return "panic";
    case ESP_RST_INT_WDT:
      return "interrupt watchdog";
    case ESP_RST_TASK_WDT:
      return "task watchdog";
    case ESP_RST_WDT:
      return "watchdog";
    case ESP_RST_DEEPSLEEP:
      return "deep sleep wake";
    case ESP_RST_BROWNOUT:
      return "brownout";
    case ESP_RST_SDIO:
      return "sdio";
    default:
      return "unknown";
  }
}

// pb_note_boot records why this boot happened and how much memory is left, so a
// device that keeps rebooting says so in the next sync instead of only showing
// the splash again.
inline void pb_note_boot() {
  std::string reason = pb_reset_reason();
  ESP_LOGI("paperboy", "reset reason: %s", reason.c_str());
  if (reason != "deep sleep wake" && reason != "power on") {
    pb_log("warn", "reset: " + reason);
  }
  size_t internal = heap_caps_get_free_size(MALLOC_CAP_INTERNAL);
  size_t psram = heap_caps_get_free_size(MALLOC_CAP_SPIRAM);
  ESP_LOGI("paperboy", "free memory: %u KiB internal, %u KiB PSRAM", (unsigned) (internal / 1024),
           (unsigned) (psram / 1024));
}

// Base URL as entered by the operator, without trailing slashes.
inline std::string pb_base(const std::string &url) {  std::string s = url;
  while (!s.empty() && (s.back() == '/' || s.back() == ' ')) {
    s.pop_back();
  }
  return s;
}

// Setup instructions, one line per '\n', starting at y.
template<typename D, typename F> void pb_draw_text(D &it, F *font, const std::string &text, int y) {
  size_t start = 0;
  while (start <= text.size()) {
    size_t end = text.find('\n', start);
    std::string line = text.substr(start, end == std::string::npos ? std::string::npos : end - start);
    it.print(24, y, font, esphome::Color::BLACK, esphome::display::TextAlign::TOP_LEFT, line.c_str(),
             esphome::Color::WHITE);
    y += 44;
    if (end == std::string::npos) {
      break;
    }
    start = end + 1;
  }
}

// The mark of Paperboy's logo (docs/logo): a page of four by three cells of 16
// units, a frame of 3 units with the top-right corner turned down, dividers of
// 2 units and one cell drawn by a widget. Every shape is filled, so the mark
// needs one colour and only spans have to be drawn; it scales to any size.
// Same geometry as the `logo` widget of the built-in plugin.
inline bool pb_logo_ink(float x, float y) {
  if (x < 0 || y < 0 || x >= 64.0f || y >= 48.0f) {
    return false;
  }
  // The page is cut at the top right: the outer path M0 0 H48 L64 16 V48 H0 Z
  // keeps every point below the diagonal x - y = 48.
  if (x - y > 48.0f) {
    return false;
  }
  // Inside the hole of the frame (the inner path, 3 units wide) only the
  // dividers, the drawn cell and the turned-down corner are ink.
  if (x >= 3.0f && x < 61.0f && y >= 3.0f && y < 45.0f && x - y <= 43.757f) {
    if ((x >= 15.0f && x < 17.0f) || (x >= 31.0f && x < 33.0f) ||
        (x >= 47.0f && x < 49.0f && y < 16.0f)) {
      return true;  // dividers between the columns
    }
    if ((y >= 15.0f && y < 17.0f) || (y >= 31.0f && y < 33.0f)) {
      return true;  // dividers between the rows
    }
    if (x < 16.0f && y >= 32.0f) {
      return true;  // the cell a widget draws
    }
    return x >= 48.0f && y < 16.0f;  // the turned-down corner
  }
  return true;
}

// Draws the mark with its top-left corner at (x, y) at the given height; the
// width follows from the design. The display lambda sets the pixels, as the QOI
// decoder does, so every panel display can draw it.
template<typename D> void pb_draw_logo(D &it, int x, int y, int height) {
  int width = (int) (height * 64.0f / 48.0f + 0.5f);
  if (width < 1 || height < 1) {
    return;
  }
  for (int row = 0; row < height; row++) {
    float v = (row + 0.5f) * 48.0f / height;
    for (int col = 0; col < width; col++) {
      if (pb_logo_ink((col + 0.5f) * 64.0f / width, v)) {
        it.draw_pixel_at(x + col, y + row, esphome::Color::BLACK);
      }
    }
  }
}

// The lockup of Paperboy's logo (docs/logo, ADR 0098): the mark, a gap and the
// wordmark in Go Mono Bold, centered at center_x with the mark's top at y. The
// wordmark is drawn at the baseline of the design, 0.80 of the height; size is
// the pixel size of the wordmark font and has to match its YAML entry.
template<typename D, typename F>
void pb_draw_lockup(D &it, int center_x, int y, int height, int size, F *wordmark) {
  int mark_width = (int) (height * 64.0f / 48.0f + 0.5f);
  int gap = (int) (height * 16.66f / 48.0f + 0.5f);
  int word_width = (int) (8 * 0.6f * size + 0.5f);  // Go Mono advances 0.6 em
  int x = center_x - (mark_width + gap + word_width) / 2;
  pb_draw_logo(it, x, y, height);
  it.print(x + mark_width + gap, y + (int) (height * 0.80f + 0.5f), wordmark, esphome::Color::BLACK,
           esphome::display::TextAlign::BASELINE_LEFT, "paperboy", esphome::Color::WHITE);
}

// The splash of a power-on: the lockup and the firmware version in the middle of
// the panel, so a device says what it is and which image runs on it.
template<typename D, typename W, typename V>
void pb_draw_splash(D &it, W *wordmark, V *version_font, const std::string &version) {
  const int mark = 132;     // the mark's height, with wordmark_large at 84 px
  const int group = 200;    // mark, gap and the version line together
  int y = (it.get_height() - group) / 2;
  pb_draw_lockup(it, it.get_width() / 2, y, mark, 84, wordmark);
  it.print(it.get_width() / 2, y + 195, version_font, esphome::Color::BLACK,
           esphome::display::TextAlign::BASELINE_CENTER, version.c_str(), esphome::Color::WHITE);
}

// The update screen: the lockup, the version the device installs and the
// status, in the middle of the panel. A device installs a firmware offer in
// place of a page, so the screen says why the panel stays quiet (ADR 0043).
template<typename D, typename W, typename V, typename F>
void pb_draw_update(D &it, W *wordmark, V *version_font, F *font, const std::string &version) {
  const int mark = 132;     // the mark's height, with wordmark_large at 84 px
  const int group = 280;    // mark, gap, version and status together
  int y = (it.get_height() - group) / 2;
  pb_draw_lockup(it, it.get_width() / 2, y, mark, 84, wordmark);
  it.print(it.get_width() / 2, y + 195, version_font, esphome::Color::BLACK,
           esphome::display::TextAlign::BASELINE_CENTER, version.c_str(), esphome::Color::WHITE);
  it.print(it.get_width() / 2, y + 235, font, esphome::Color::BLACK,
           esphome::display::TextAlign::TOP_CENTER, "Installing update…", esphome::Color::WHITE);
}

// The Wi-Fi and Paperboy URL screens: the lockup as a header (wordmark_small at
// 36 px), the version in the corner and the instructions below it (pb_draw_text,
// 44 px per line).
template<typename D, typename F, typename W, typename V>
void pb_draw_setup(D &it, F *font, W *wordmark, V *version_font, const std::string &version,
                   const std::string &text) {
  pb_draw_lockup(it, it.get_width() / 2, 16, 56, 36, wordmark);
  it.print(it.get_width() - 24, 48, version_font, esphome::Color::BLACK,
           esphome::display::TextAlign::BASELINE_RIGHT, version.c_str(), esphome::Color::WHITE);
  pb_draw_text(it, font, text, 96);
}

// Paperboy draws its four levels as 0, 85, 170 and 255.
inline int pb_level(uint8_t v) { return v < 43 ? 0 : v < 128 ? 1 : v < 213 ? 2 : 3; }

// The QOI page the last sync asked for (ADR 0072). The display lambda paints
// it: ESPHome's own decoders take 2.2-2.6 s for the 384k pixels of a page
// because they convert and draw it pixel by pixel, so the firmware decodes the
// page itself and writes it straight into the panel buffer.
static std::string pb_page_qoi;

inline int pb_qoi_slot(uint32_t r, uint32_t g, uint32_t b, uint32_t a) {
  return (r * 3 + g * 5 + b * 7 + a * 11) % 64;
}

// Walks a QOI stream and calls put(x, y, r, g, b) for every pixel. False for a
// stream that is not QOI or ends early, so the caller reports it instead of
// painting garbage.
template<typename F> inline bool pb_qoi_each(const std::string &data, F &&put) {
  const uint8_t *p = reinterpret_cast<const uint8_t *>(data.data());
  const size_t size = data.size();
  if (size < 14 || std::memcmp(p, "qoif", 4) != 0) {
    return false;
  }
  const size_t width = (static_cast<uint32_t>(p[4]) << 24) | (static_cast<uint32_t>(p[5]) << 16) |
                       (static_cast<uint32_t>(p[6]) << 8) | p[7];
  const size_t height = (static_cast<uint32_t>(p[8]) << 24) | (static_cast<uint32_t>(p[9]) << 16) |
                        (static_cast<uint32_t>(p[10]) << 8) | p[11];
  if (width == 0 || height == 0 || p[12] < 3 || p[12] > 4 || p[13] > 1) {
    return false;
  }
  const size_t total = width * height;
  uint8_t index[64][4];
  std::memset(index, 0, sizeof(index));
  uint8_t r = 0, g = 0, b = 0, a = 255;
  size_t i = 14, n = 0;
  auto pixel = [&]() {
    put(n % width, n / width, r, g, b);
    n++;
  };
  while (n < total) {
    if (i >= size) {
      return false;
    }
    const uint8_t byte = p[i];
    if (byte == 0xfe) {
      if (i + 3 >= size) {
        return false;
      }
      r = p[i + 1];
      g = p[i + 2];
      b = p[i + 3];
      i += 4;
      pixel();
    } else if (byte == 0xff) {
      if (i + 4 >= size) {
        return false;
      }
      r = p[i + 1];
      g = p[i + 2];
      b = p[i + 3];
      a = p[i + 4];
      i += 5;
      pixel();
    } else if ((byte & 0xc0) == 0x00) {
      const uint8_t *c = index[byte & 0x3f];
      r = c[0];
      g = c[1];
      b = c[2];
      a = c[3];
      i++;
      pixel();
    } else if ((byte & 0xc0) == 0x40) {
      r += ((byte >> 4) & 0x03) - 2;
      g += ((byte >> 2) & 0x03) - 2;
      b += (byte & 0x03) - 2;
      i++;
      pixel();
    } else if ((byte & 0xc0) == 0x80) {
      if (i + 1 >= size) {
        return false;
      }
      const int dg = (byte & 0x3f) - 32;
      r += dg - 8 + ((p[i + 1] >> 4) & 0x0f);
      g += dg;
      b += dg - 8 + (p[i + 1] & 0x0f);
      i += 2;
      pixel();
    } else {
      const size_t run = (byte & 0x3f) + 1;
      i++;
      for (size_t k = 0; k < run && n < total; k++) {
        pixel();
      }
    }
    uint8_t *slot = index[pb_qoi_slot(r, g, b, a)];
    slot[0] = r;
    slot[1] = g;
    slot[2] = b;
    slot[3] = a;
  }
  return true;
}

inline int pb_qoi_level(uint8_t r, uint8_t g, uint8_t b) {
  return pb_level(static_cast<uint8_t>((r * 77 + g * 150 + b * 29) >> 8));
}

// A complete QOI stream, parsed without painting. The response buffer is a
// fixed size (max_response_buffer_size in common.yaml, 512 KiB), and
// http_request truncates a larger answer, which would leave half a page on the
// panel: the caller validates first and keeps the old picture instead. Paperboy
// keeps every page below that buffer (ADR 0088), so this is the last resort.
inline bool pb_qoi_valid(const std::string &data) {
  return pb_qoi_each(data, [](int, int, uint8_t, uint8_t, uint8_t) {});
}

// pb_qoi_size reads the dimensions of a QOI header; false when the stream does
// not start with one.
inline bool pb_qoi_size(const std::string &data, int &width, int &height) {
  const uint8_t *p = reinterpret_cast<const uint8_t *>(data.data());
  if (data.size() < 14 || std::memcmp(p, "qoif", 4) != 0) {
    return false;
  }
  width = static_cast<int>((static_cast<uint32_t>(p[4]) << 24) |
                           (static_cast<uint32_t>(p[5]) << 16) |
                           (static_cast<uint32_t>(p[6]) << 8) | p[7]);
  height = static_cast<int>((static_cast<uint32_t>(p[8]) << 24) |
                            (static_cast<uint32_t>(p[9]) << 16) |
                            (static_cast<uint32_t>(p[10]) << 8) | p[11]);
  return true;
}

// pb_qoi_fits reports whether the stream names exactly the size the rotated
// display expects. draw_pixel_at() clips silently, so a page of the wrong
// orientation would leave the rest of the previous picture on the panel; the
// caller notes it instead of painting half a page (issue #117).
inline bool pb_qoi_fits(const std::string &data, int width, int height) {
  int w = 0, h = 0;
  return pb_qoi_size(data, w, h) && w == width && h == height;
}

// A page the firmware cannot show becomes a full-screen notice in the layout of
// the setup screen, so the device says what is wrong instead of keeping a stale
// picture. The text is set by the display lambda or by a failed download.
static std::string pb_panel_notice;

inline void pb_set_panel_notice(const std::string &text) {
  pb_panel_notice = text;
  ESP_LOGE("paperboy", "panel notice: %s", text.c_str());
}

// Draws the notice page: the lockup at the top, the version in its corner and
// the text centered below it, one line per '\n'.
template<typename D, typename F, typename W, typename V>
void pb_draw_notice_page(D &it, F *font, W *wordmark, V *version_font, const std::string &version,
                         const std::string &text) {
  it.fill(esphome::Color::WHITE);
  pb_draw_lockup(it, it.get_width() / 2, 48, 84, 56, wordmark);
  it.print(it.get_width() - 48, 48 + 56, version_font, esphome::Color::BLACK,
           esphome::display::TextAlign::BASELINE_RIGHT, version.c_str(), esphome::Color::WHITE);
  int lines = 1;
  for (char c : text) {
    if (c == '\n') {
      lines++;
    }
  }
  const int spacing = 72;
  int y = it.get_height() / 2 - (lines - 1) * spacing / 2;
  size_t start = 0;
  while (start <= text.size()) {
    size_t end = text.find('\n', start);
    std::string line = text.substr(start, end == std::string::npos ? std::string::npos : end - start);
    it.print(96, y, font, esphome::Color::BLACK, esphome::display::TextAlign::TOP_LEFT, line.c_str(),
             esphome::Color::WHITE);
    y += spacing;
    if (end == std::string::npos) {
      break;
    }
    start = end + 1;
  }
}

// Four-level panel, used by both hardware types that draw pages: the Sticky's
// ESPHome model and the E1001's uc8179_gray4 component (ADR 0086). 64 and 128
// sit inside the luma bands 32/96/160 that ESPHome's gray4 model uses for dark
// gray and light gray; uc8179_gray4 quantizes with luma >> 6, where 64 becomes
// dark gray and 128 light gray. White is left alone: the caller starts from a
// white buffer.
template<typename D> inline bool pb_paint_qoi_gray4(D &it, const std::string &data) {
  static const uint8_t band[4] = {0, 64, 128, 255};
  return pb_qoi_each(data, [&it](int x, int y, uint8_t r, uint8_t g, uint8_t b) {
    const int l = pb_qoi_level(r, g, b);
    if (l != 3) {
      it.draw_pixel_at(x, y, esphome::Color(band[l], band[l], band[l]));
    }
  });
}

// 16-level panel, used by the E1003's IT8951 controller (issue #192). The
// driver turns the luma of a pixel into one of 16 nibbles, so a page with the
// four levels Paperboy composes today draws at 0, 85, 170 and 255, and a page
// with more levels keeps them. White is left alone: the caller starts from a
// white buffer.
template<typename D> inline bool pb_paint_qoi_gray16(D &it, const std::string &data) {
  return pb_qoi_each(data, [&it](int x, int y, uint8_t r, uint8_t g, uint8_t b) {
    const uint8_t luma = static_cast<uint8_t>((r * 77 + g * 150 + b * 29) >> 8);
    if (luma != 255) {
      it.draw_pixel_at(x, y, esphome::Color(luma, luma, luma));
    }
  });
}

// Black-and-white panel: dark gray as 3 of 4 pixels black, light gray as 1 of
// 4, in a fixed 2x2 pattern so repeated paints stay identical. No shipped image
// display uses this since fw 2.4.0 (the E1001 draws real gray levels, ADR 0086);
// it stays for a mono panel and as the fallback if a batch cannot do grays.
template<typename D> inline bool pb_paint_qoi_dithered(D &it, const std::string &data) {
  return pb_qoi_each(data, [&it](int x, int y, uint8_t r, uint8_t g, uint8_t b) {
    const int l = pb_qoi_level(r, g, b);
    const int cell = (y & 1) * 2 + (x & 1);  // 0..3
    if (l == 0 || (l == 1 && cell != 3) || (l == 2 && cell == 0)) {
      it.draw_pixel_at(x, y, esphome::Color::BLACK);
    }
  });
}


// ---------------------------------------------------------------- text displays (ADR 0044)

// Lines on the text display and the hash Paperboy gave them.
static std::vector<std::string> pb_lines;
static std::string pb_text_hash;
// True when pb_take_text replaced the text; the YAML then repaints and starts
// or stops the marquee.
static bool pb_text_changed = false;

enum PbScrollDirection {
  PB_SCROLL_HORIZONTAL = 0,
  PB_SCROLL_VERTICAL = 1,
  PB_SCROLL_BOTH = 2,
};

// One scrolling text box (ADR 0117). The server names its position and size in
// characters, the full lines, the first window, the direction and the step.
struct PbScrollBox {
  int row = 0;
  int col = 0;
  int width = 0;
  int height = 1;
  int direction = PB_SCROLL_HORIZONTAL;
  std::string align = "left";
  uint32_t step_ms = 300;
  uint32_t offset = 0;   // characters for horizontal, lines for vertical
  uint32_t next_ms = 0;  // millis() of the next step
  std::vector<std::string> lines;   // full content, not cut to the box
  std::vector<std::string> window;  // first visible lines, aligned by the server
};

static std::vector<PbScrollBox> pb_text_boxes;

inline int pb_columns(const std::string &s);

inline std::string pb_json_text(JsonVariant v) {
  const char *s = v.as<const char *>();
  return s ? std::string(s) : std::string();
}

inline int pb_scroll_direction(const std::string &name) {
  if (name == "vertical") return PB_SCROLL_VERTICAL;
  if (name == "both") return PB_SCROLL_BOTH;
  return PB_SCROLL_HORIZONTAL;
}

inline void pb_reset_scroll() {
  uint32_t now = esphome::millis();
  for (auto &box : pb_text_boxes) {
    box.offset = 0;
    box.next_ms = now + box.step_ms;
  }
}

// Takes the text object of a sync or text poll answer; true when it changed.
// scroll_changed keeps the running offsets when only other cells changed, and
// resets them when the scrolling text itself changed (ADR 0117).
inline bool pb_take_text(JsonObject text) {
  std::string hash = text["hash"] | "";
  if (hash.empty()) {
    return false;
  }
  bool scroll_changed = text["scroll_changed"] | false;
  if (hash == pb_text_hash) {
    if (scroll_changed && !pb_text_boxes.empty()) {
      pb_reset_scroll();
      return true;
    }
    return false;
  }
  pb_lines.clear();
  JsonArray lines = text["lines"].as<JsonArray>();
  for (JsonVariant v : lines) {
    pb_lines.push_back(pb_json_text(v));
  }
  std::vector<PbScrollBox> boxes;
  JsonArray scrolls = text["scrolls"].as<JsonArray>();
  if (!scrolls.isNull()) {
    for (JsonVariant v : scrolls) {
      JsonObject o = v.as<JsonObject>();
      PbScrollBox box;
      box.row = o["row"] | 0;
      box.col = o["col"] | 0;
      box.width = o["width"] | 0;
      box.height = o["height"] | 1;
      if (box.row < 0 || box.col < 0 || box.width < 1 || box.height < 1) {
        continue;
      }
      box.step_ms = (uint32_t) std::max(50, (int) (o["step_ms"] | 300));
      box.direction = pb_scroll_direction(o["direction"] | "horizontal");
      box.align = o["align"] | "left";
      JsonArray full = o["text"].as<JsonArray>();
      for (JsonVariant line : full) {
        box.lines.push_back(pb_json_text(line));
      }
      JsonArray window = o["window"].as<JsonArray>();
      for (JsonVariant line : window) {
        box.window.push_back(pb_json_text(line));
      }
      if (!box.lines.empty()) {
        boxes.push_back(box);
      }
    }
  } else {
    // Older servers send one full line per scrolling row and no step (ADR 0092).
    int row = 0;
    JsonArray legacy = text["scroll"].as<JsonArray>();
    for (JsonVariant v : legacy) {
      std::string full = pb_json_text(v);
      if (!full.empty()) {
        PbScrollBox box;
        box.row = row;
        box.width = row < (int) pb_lines.size() ? std::max(1, pb_columns(pb_lines[row])) : std::max(1, pb_columns(full));
        box.step_ms = 300;
        box.direction = PB_SCROLL_HORIZONTAL;
        box.lines.push_back(full);
        if (row < (int) pb_lines.size()) {
          box.window.push_back(pb_lines[row]);
        }
        boxes.push_back(box);
      }
      row++;
    }
  }
  // The same boxes keep their offset and their phase; a moved or resized box
  // starts over, like a changed scroll text.
  bool same = boxes.size() == pb_text_boxes.size();
  if (same) {
    for (size_t i = 0; i < boxes.size(); i++) {
      const PbScrollBox &old = pb_text_boxes[i];
      if (old.row != boxes[i].row || old.col != boxes[i].col || old.width != boxes[i].width ||
          old.height != boxes[i].height || old.direction != boxes[i].direction ||
          old.lines.size() != boxes[i].lines.size()) {
        same = false;
        break;
      }
    }
  }
  uint32_t now = esphome::millis();
  for (size_t i = 0; i < boxes.size(); i++) {
    if (!scroll_changed && same) {
      boxes[i].offset = pb_text_boxes[i].offset;
      boxes[i].next_ms = pb_text_boxes[i].next_ms;
    } else {
      boxes[i].offset = 0;
      boxes[i].next_ms = now + boxes[i].step_ms;
    }
  }
  pb_text_boxes = std::move(boxes);
  pb_text_hash = hash;
  pb_remember(hash);
  return true;
}

// Shows a local message, e.g. setup instructions, one line per '\n'.
inline void pb_show_message(const std::string &text) {
  pb_lines.clear();
  size_t start = 0;
  while (start <= text.size()) {
    size_t end = text.find('\n', start);
    pb_lines.push_back(text.substr(start, end == std::string::npos ? std::string::npos : end - start));
    if (end == std::string::npos) {
      break;
    }
    start = end + 1;
  }
  pb_text_boxes.clear();
  pb_text_hash.clear();
}

// pb_columns counts the characters of a UTF-8 string, not its bytes: the
// display counts columns, and the server converted the text to its charset.
inline int pb_columns(const std::string &s) {
  int n = 0;
  for (size_t i = 0; i < s.size(); i++) {
    if (((unsigned char) s[i] & 0xC0) != 0x80) n++;
  }
  return n;
}

// pb_mid returns count characters of s starting at character start.
inline std::string pb_mid(const std::string &s, int start, int count) {
  std::string out;
  int n = 0;
  for (size_t i = 0; i < s.size();) {
    size_t next = i + 1;
    while (next < s.size() && ((unsigned char) s[next] & 0xC0) == 0x80) next++;
    if (n >= start && n < start + count) out.append(s, i, next - i);
    n++;
    i = next;
    if (n >= start + count) break;
  }
  return out;
}

// pb_pad fills a line up to cols characters so the LCD has no leftovers from
// the previous content; a longer line stays as it is (setup instructions).
inline std::string pb_pad(const std::string &s, int cols) {
  std::string out = s;
  int n = pb_columns(out);
  if (n < cols) out.append(cols - n, ' ');
  return out;
}

// pb_replace_columns writes text into a display row at a character column and
// keeps the characters before and after the box.
inline std::string pb_replace_columns(const std::string &line, int col, int width, const std::string &text) {
  const int n = pb_columns(line);
  std::string out = pb_mid(line, 0, col);
  out += pb_pad(pb_mid(text, 0, width), width);
  const int end = col + width;
  if (end < n) {
    out += pb_mid(line, end, n - end);
  }
  return out;
}

// pb_align_to pads a line to the box width with the alignment the server sent;
// a longer line is cut. Decimal alignment falls back to right for a line.
inline std::string pb_align_to(const std::string &s, int width, const std::string &align) {
  int n = pb_columns(s);
  if (n >= width) {
    return pb_mid(s, 0, width);
  }
  std::string gap(width - n, ' ');
  return align == "right" || align == "decimal" ? gap + s : s + gap;
}

// pb_has_scroll reports whether the current page has a scrolling box.
inline bool pb_has_scroll() { return !pb_text_boxes.empty(); }

// pb_scroll_line returns the characters one line of a scrolling box shows now.
// Horizontal moves the window over the line, vertical moves the window down by
// lines, and both move vertically first and then horizontally (ADR 0117).
inline std::string pb_scroll_line(const PbScrollBox &box, int y, int width) {
  const int count = (int) box.lines.size();
  int index = y;
  if (box.direction != PB_SCROLL_HORIZONTAL && count > 0) {
    index = (int) ((box.offset + (uint32_t) y) % (uint32_t) count);
  }
  std::string line = index >= 0 && index < count ? box.lines[index] : "";
  const bool horizontal = box.direction == PB_SCROLL_HORIZONTAL || box.direction == PB_SCROLL_BOTH;
  if (horizontal && pb_columns(line) > width) {
    // Two spaces separate the repetitions of the marquee.
    std::string marquee = line + "  ";
    const int length = std::max(1, pb_columns(marquee));
    std::string out = pb_mid(marquee, (int) (box.offset % (uint32_t) length), width);
    if (pb_columns(out) < width) {
      out += pb_mid(marquee, 0, width - pb_columns(out));
    }
    return out;
  }
  if (box.direction == PB_SCROLL_HORIZONTAL && y < (int) box.window.size()) {
    return pb_pad(pb_mid(box.window[y], 0, width), width);
  }
  return pb_align_to(line, width, box.align);
}

// pb_row returns the cols characters a row shows now: the static line with the
// window of every scrolling box over it.
inline std::string pb_row(int row, int cols) {
  std::string line = row >= 0 && row < (int) pb_lines.size() ? pb_lines[row] : "";
  for (const auto &box : pb_text_boxes) {
    if (row < box.row || row >= box.row + box.height) {
      continue;
    }
    line = pb_replace_columns(line, box.col, box.width, pb_scroll_line(box, row - box.row, box.width));
  }
  return pb_pad(pb_mid(line, 0, cols), cols);
}

// pb_scroll_wait_ms is the delay to the next due box, at least 1 ms.
inline uint32_t pb_scroll_wait_ms() {
  if (pb_text_boxes.empty()) {
    return 1000;
  }
  const uint32_t now = esphome::millis();
  uint32_t wait = 60000;
  for (const auto &box : pb_text_boxes) {
    const int32_t due = (int32_t) (box.next_ms - now);
    if (due <= 0) {
      return 1;
    }
    if ((uint32_t) due < wait) {
      wait = (uint32_t) due;
    }
  }
  return wait;
}

// pb_scroll_tick advances every box whose step is due and reports whether the
// display has to be repainted. No loop here: the YAML script waits between
// steps, so Wi-Fi and OTA keep running.
inline bool pb_scroll_tick() {
  if (pb_text_boxes.empty()) {
    return false;
  }
  const uint32_t now = esphome::millis();
  bool moved = false;
  for (auto &box : pb_text_boxes) {
    if ((int32_t) (box.next_ms - now) <= 0) {
      box.offset++;
      box.next_ms = now + box.step_ms;
      moved = true;
    }
  }
  return moved;
}

// Maps the UTF-8 characters Paperboy keeps for charset hd44780 to the codes
// of the HD44780 A00 character ROM; anything else outside ASCII becomes '?'.
inline std::string pb_hd44780(const std::string &s) {
  std::string out;
  for (size_t i = 0; i < s.size(); i++) {
    unsigned char c = s[i];
    if (c < 0x80) {
      out += (char) c;
      continue;
    }
    unsigned char n = i + 1 < s.size() ? (unsigned char) s[i + 1] : 0;
    char mapped = '?';
    if (c == 0xC3 && n == 0xA4) mapped = (char) 0xE1;       // ä
    else if (c == 0xC3 && n == 0xB6) mapped = (char) 0xEF;  // ö
    else if (c == 0xC3 && n == 0xBC) mapped = (char) 0xF5;  // ü
    else if (c == 0xC3 && n == 0x9F) mapped = (char) 0xE2;  // ß
    else if (c == 0xC2 && n == 0xB0) mapped = (char) 0xDF;  // °
    else if (c == 0xC2 && n == 0xB5) mapped = (char) 0xE4;  // µ
    // The A00 ROM has no capital umlauts: keep the base letter.
    else if (c == 0xC3 && n == 0x84) mapped = 'A';          // Ä
    else if (c == 0xC3 && n == 0x96) mapped = 'O';          // Ö
    else if (c == 0xC3 && n == 0x9C) mapped = 'U';          // Ü
    out += mapped;
    // Skip the continuation bytes of this character.
    while (i + 1 < s.size() && ((unsigned char) s[i + 1] & 0xC0) == 0x80) {
      i++;
    }
  }
  return out;
}
