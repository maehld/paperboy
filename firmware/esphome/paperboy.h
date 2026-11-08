// Helpers for the Paperboy firmware (ADR 0041), included by common.yaml. All
// knowledge of the device API (ADR 0042) that is easier in C++ than in YAML
// lives here: the sync body, the event queue and the image bookkeeping.
#pragma once

#include "esphome.h"
#include <cmath>
#include <cstring>
#include <string>
#include <vector>

// Kept in RTC memory: survives deep sleep, cleared on power-on, and costs no
// flash writes per wake (unlike globals with restore_value).
RTC_DATA_ATTR static char pb_shown_hash[32];  // hash of the image on the panel
RTC_DATA_ATTR static int pb_refresh_s;         // last refresh_s from Paperboy
RTC_DATA_ATTR static char pb_note[120];        // one warning for the next sync, e.g. a failed download

// Event queue and identities (ADR 0064). They live in RTC memory so that a
// failed sync keeps its events and resends them with the same IDs.
RTC_DATA_ATTR static uint32_t pb_event_seq;      // last assigned event ID
RTC_DATA_ATTR static char pb_event_queue[1200];  // queued events as JSON objects, comma separated
RTC_DATA_ATTR static char pb_next_reason[16];    // reason of the next wake: menu or result
RTC_DATA_ATTR static uint32_t pb_next_s;         // seconds until that one-shot wake

// Guards the RTC layout. RTC memory survives a reboot, so after a firmware
// update the new variables above would read whatever the old image left there:
// a queue without a terminator turns every sync into invalid JSON. The marker
// is declared last, so all older variables keep their offsets; bump it whenever
// this block changes.
RTC_DATA_ATTR static uint32_t pb_rtc_magic;
constexpr uint32_t PB_RTC_MAGIC = 0x50423031;  // "PB01"

// Zeroes the RTC state once per layout. Safe to call on every boot.
inline void pb_init_rtc() {
  if (pb_rtc_magic == PB_RTC_MAGIC) {
    return;
  }
  pb_rtc_magic = PB_RTC_MAGIC;
  pb_event_seq = 0;
  pb_event_queue[0] = 0;
  pb_next_reason[0] = 0;
  pb_next_s = 0;
  pb_note[0] = 0;
  // The panel shows an image of the previous firmware: repaint it.
  pb_shown_hash[0] = 0;
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

// Battery reading of this wake; NAN = not measured. Set by the device file's
// read_battery script before any decision uses it.
static float pb_bat_volts = NAN;
static float pb_bat_percent = NAN;
static float pb_bat_mah = NAN;
static float pb_bat_ma = NAN;

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

inline void pb_button(const std::string &role) {
  pb_event_seq++;
  if (!pb_queue_event("{\"id\":" + std::to_string(pb_event_seq) + ",\"type\":\"button\",\"button\":" +
                      pb_json_string(role) + "}")) {
    pb_note_for_next_wake("event queue full");
  }
}

// One tap is one event: the panel reports a position continuously while a
// finger rests on it, so the first report within touchGap is taken and the
// rest are dropped (ADR 0064).
inline void pb_touch(int x, int y) {
  static uint32_t last_ms;
  uint32_t now_ms = esphome::millis();
  if (last_ms != 0 && now_ms - last_ms < 800) {
    return;
  }
  last_ms = now_ms;
  pb_event_seq++;
  if (!pb_queue_event(esphome::str_sprintf("{\"id\":%lu,\"type\":\"touch\",\"x\":%d,\"y\":%d}",
                                           (unsigned long) pb_event_seq, x, y))) {
    pb_note_for_next_wake("event queue full");
  }
}

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
  if (std::isfinite(pb_bat_volts) && pb_bat_volts > 0) {
    b += esphome::str_sprintf(",\"battery\":{\"volts\":%.3f", pb_bat_volts);
    if (std::isfinite(pb_bat_percent))
      b += esphome::str_sprintf(",\"percent\":%.0f", pb_bat_percent);
    if (std::isfinite(pb_bat_mah))
      b += esphome::str_sprintf(",\"mah\":%.1f", pb_bat_mah);
    if (std::isfinite(pb_bat_ma))
      b += esphome::str_sprintf(",\"ma\":%.1f", pb_bat_ma);
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

// pb_sync_done drops what Paperboy confirmed: the event queue and the logs.
inline void pb_sync_done() {
  pb_event_queue[0] = 0;
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

// Base URL as entered by the operator, without trailing slashes.
inline std::string pb_base(const std::string &url) {
  std::string s = url;
  while (!s.empty() && (s.back() == '/' || s.back() == ' ')) {
    s.pop_back();
  }
  return s;
}

// Setup instructions, one line per '\n'.
template<typename D, typename F> void pb_draw_text(D &it, F *font, const std::string &text) {
  int y = 24;
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

// Paperboy draws its four levels as 0, 85, 170 and 255.
inline int pb_level(uint8_t v) { return v < 43 ? 0 : v < 128 ? 1 : v < 213 ? 2 : 3; }

// Four-level panel (Sticky gray4): ESPHome bands luma at 32/96/160, so 170
// would turn white. Draw each level inside its band.
template<typename D, typename I> void pb_draw_gray4(D &it, I *img) {
  static const uint8_t band[4] = {0, 64, 128, 255};
  for (int y = 0; y < img->get_height(); y++) {
    for (int x = 0; x < img->get_width(); x++) {
      int l = pb_level(img->get_pixel(x, y, esphome::Color::WHITE, esphome::Color::BLACK).red);
      if (l != 3) {
        it.draw_pixel_at(x, y, esphome::Color(band[l], band[l], band[l]));
      }
    }
  }
}

// Black-and-white panel: dark gray as 3 of 4 pixels black, light gray as
// 1 of 4, in a fixed 2x2 pattern so repeated paints stay identical.
template<typename D, typename I> void pb_draw_dithered(D &it, I *img) {
  for (int y = 0; y < img->get_height(); y++) {
    for (int x = 0; x < img->get_width(); x++) {
      int l = pb_level(img->get_pixel(x, y, esphome::Color::WHITE, esphome::Color::BLACK).red);
      int cell = (y & 1) * 2 + (x & 1);  // 0..3
      bool black = l == 0 || (l == 1 && cell != 3) || (l == 2 && cell == 0);
      if (black) {
        it.draw_pixel_at(x, y, esphome::Color::BLACK);
      }
    }
  }
}

// ---------------------------------------------------------------- text displays (ADR 0044)

// Lines on the text display and the hash Paperboy gave them.
static std::vector<std::string> pb_lines;
static std::string pb_text_hash;

// Takes the text object of a sync or text poll answer; true when it changed.
inline bool pb_take_text(JsonObject text) {
  std::string hash = text["hash"] | "";
  if (hash.empty() || hash == pb_text_hash) {
    return false;
  }
  pb_lines.clear();
  for (JsonVariant v : text["lines"].as<JsonArray>()) {
    pb_lines.push_back(v.as<const char *>() ? v.as<const char *>() : "");
  }
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
  pb_text_hash.clear();
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
