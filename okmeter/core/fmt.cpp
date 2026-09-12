#include "fmt.h"
#include <cstdio>
#include <cstring>

namespace okmeter {

std::string fmtCompact(int64_t v) {
  char buf[24];
  if (v >= 1000000) { std::snprintf(buf, sizeof buf, "%.1fM", v / 1e6); return buf; }
  if (v >= 1000)    { std::snprintf(buf, sizeof buf, "%.1fK", v / 1e3); return buf; }
  std::snprintf(buf, sizeof buf, "%lld", (long long)v);
  return buf;
}

std::string fmtExact(int64_t v) {
  if (v < 0) v = 0;
  char digits[24];
  std::snprintf(digits, sizeof digits, "%lld", (long long)v);
  std::string out;
  int len = (int)std::strlen(digits);
  for (int i = 0; i < len; ++i) {
    if (i > 0 && (len - i) % 3 == 0) out += ',';
    out += digits[i];
  }
  return out;
}

std::string fmtYi(int64_t v) {
  if (v < 0) v = 0;
  char buf[24];
  if (v >= 100000000) { std::snprintf(buf, sizeof buf, "%.2f亿", v / 1e8); return buf; }
  if (v >= 10000)     { std::snprintf(buf, sizeof buf, "%.1f万", v / 1e4); return buf; }
  std::snprintf(buf, sizeof buf, "%lld", (long long)v);
  return buf;
}

std::string relTime(int64_t thenMs, int64_t nowMs) {
  int64_t s = (nowMs - thenMs) / 1000;
  if (s < 0) s = 0;
  if (s < 60) return "刚刚";
  const int64_t m = s / 60;
  if (m < 60) return std::to_string(m) + " 分钟前";
  const int64_t h = m / 60;
  if (h < 24) return std::to_string(h) + " 小时前";
  return std::to_string(h / 24) + " 天前";
}

} // namespace okmeter
