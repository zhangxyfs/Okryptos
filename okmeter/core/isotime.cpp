#define _CRT_SECURE_NO_WARNINGS  // sscanf 解析固定格式时间串（MSVC C4996 降噪）
#include "isotime.h"
#include <cstdio>
#include <cstdlib>
#include <ctime>

namespace okmeter {

int64_t isoToMs(const std::string& iso) {
  int y = 0, mo = 0, d = 0, h = 0, mi = 0, s = 0;
  if (std::sscanf(iso.c_str(), "%d-%d-%dT%d:%d:%d", &y, &mo, &d, &h, &mi, &s) < 6)
    return 0;
  // 小数秒（.fff，至多取 3 位，不足右补零）
  int ms = 0;
  size_t dot = iso.find('.', 19);
  if (dot != std::string::npos) {
    int scale = 100;
    for (size_t i = dot + 1; i < iso.size() && scale > 0; ++i) {
      const char c = iso[i];
      if (c < '0' || c > '9') break;
      ms += (c - '0') * scale;
      scale /= 10;
    }
  }
  // 时区：'Z'/缺省 = UTC；'±hh:mm' 为本地偏移（epoch = 本地 - 偏移）
  int offMin = 0;
  const size_t tz = iso.find_first_of("Z+-", 19);
  if (tz != std::string::npos && iso[tz] != 'Z') {
    int oh = 0, om = 0;
    if (std::sscanf(iso.c_str() + tz + 1, "%d:%d", &oh, &om) >= 1)
      offMin = (iso[tz] == '+' ? 1 : -1) * (oh * 60 + om);
  }
  std::tm t{};
  t.tm_year = y - 1900;
  t.tm_mon = mo - 1;
  t.tm_mday = d;
  t.tm_hour = h;
  t.tm_min = mi;
  t.tm_sec = s;
  const int64_t sec = (int64_t)_mkgmtime(&t);
  if (sec < 0) return 0;
  return sec * 1000 + ms - (int64_t)offMin * 60000;
}

} // namespace okmeter
