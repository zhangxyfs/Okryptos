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
  char digits[24];
  std::snprintf(digits, sizeof digits, "%lld", (long long)v);
  std::string d = digits;
  // 单位只作分隔：亿=右数 8 位、万=右数 4 位处插小数点，全位数保留，尾部零修剪
  const char* unit = nullptr;
  size_t cut = 0;
  if (v >= 100000000 && d.size() > 8) { unit = "亿"; cut = d.size() - 8; }
  else if (v >= 10000 && d.size() > 4) { unit = "万"; cut = d.size() - 4; }
  if (!unit) return d;
  std::string out = d.substr(0, cut) + "." + d.substr(cut);
  while (!out.empty() && out.back() == '0') out.pop_back();  // 21.00000000 → 21
  if (!out.empty() && out.back() == '.') out.pop_back();
  return out + unit;
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
