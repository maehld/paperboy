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

// Battery reading of this wake; NAN = not measured. Set by the device file's
// read_battery script before any decision uses it.
static float pb_bat_volts = NAN;
static float pb_bat_percent = NAN;
static float pb_bat_mah = NAN;
static float pb_bat_ma = NAN;

// Events since the last sync, as JSON objects, and log entries for the next sync.
static std::vector<std::string> pb_events;
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

inline void pb_button(const std::string &role) {
  pb_events.push_back("{\"type\":\"button\",\"button\":" + pb_json_string(role) + "}");
}

inline void pb_touch(int x, int y) {
  pb_events.push_back(esphome::str_sprintf("{\"type\":\"touch\",\"x\":%d,\"y\":%d}", x, y));
}

inline void pb_log(const std::string &level, const std::string &message) {
  if (pb_logs.size() < 16) {
    pb_logs.push_back("{\"level\":" + pb_json_string(level) + ",\"message\":" + pb_json_string(message) +
                      ",\"source\":\"firmware\"}");
  }
}

// Remembers a warning across deep sleep, so the next sync can report it.
inline void pb_note_for_next_wake(const std::string &message) {
  std::strncpy(pb_note, message.c_str(), sizeof(pb_note) - 1);
  pb_note[sizeof(pb_note) - 1] = 0;
}

inline bool pb_has_events() { return !pb_events.empty(); }

// Builds the sync body and empties the event and log queues.
inline std::string pb_sync_body(const std::string &firmware, const std::string &hardware, const std::string &reason,
                                int rssi) {
  if (pb_note[0]) {
    pb_log("warn", pb_note);
    pb_note[0] = 0;
  }
  std::string b = "{\"firmware\":" + pb_json_string(firmware) + ",\"hardware\":" + pb_json_string(hardware) +
                  ",\"reason\":" + pb_json_string(reason) + ",\"shown\":" + pb_json_string(pb_shown_hash);
  b += ",\"events\":[";
  for (size_t i = 0; i < pb_events.size(); i++) {
    b += (i ? "," : "") + pb_events[i];
  }
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
  b += ",\"log\":[";
  for (size_t i = 0; i < pb_logs.size(); i++) {
    b += (i ? "," : "") + pb_logs[i];
  }
  b += "]}";
  pb_events.clear();
  pb_logs.clear();
  return b;
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
