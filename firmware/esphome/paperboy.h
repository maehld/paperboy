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

// pb_button queues one button event. hold_ms is the measured press duration;
// the server decides from it whether the press was short or long (ADR 0067).
inline void pb_button(const std::string &role, uint32_t hold_ms) {
  pb_event_seq++;
  if (!pb_queue_event("{\"id\":" + std::to_string(pb_event_seq) + ",\"type\":\"button\",\"button\":" +
                      pb_json_string(role) + ",\"hold_ms\":" + std::to_string(hold_ms) + "}")) {
    pb_note_for_next_wake("event queue full");
  }
}

// One contact is one event. The panel reports a position continuously while a
// finger rests on it, so a tap is the first report of a contact and every
// further report is dropped until the finger lifts (on_release). The old
// 800 ms gap also swallowed a deliberate second tap (issue #136). A contact
// that reports nothing for ten seconds counts as lifted, so a missed release
// cannot mute the panel.
static bool pb_touch_down = false;
static uint32_t pb_touch_last_ms = 0;

inline void pb_touch(int x, int y) {
  uint32_t now_ms = esphome::millis();
  if (pb_touch_down && now_ms - pb_touch_last_ms < 10000) {
    pb_touch_last_ms = now_ms;
    return;
  }
  pb_touch_down = true;
  pb_touch_last_ms = now_ms;
  pb_event_seq++;
  if (!pb_queue_event(esphome::str_sprintf("{\"id\":%lu,\"type\":\"touch\",\"x\":%d,\"y\":%d}",
                                           (unsigned long) pb_event_seq, x, y))) {
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
