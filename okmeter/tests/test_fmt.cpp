#include "../core/fmt.h"
#include "framework.h"

using namespace okmeter;

TEST(fmt_compact_thresholds) {
  CHECK(fmtCompact(0) == "0");
  CHECK(fmtCompact(999) == "999");
  CHECK(fmtCompact(9900) == "9.9K");
  CHECK(fmtCompact(128590) == "128.6K");
  CHECK(fmtCompact(2134756) == "2.1M");
  CHECK(fmtCompact(1000000) == "1.0M");
}

TEST(fmt_exact_grouping) {
  CHECK(fmtExact(0) == "0");
  CHECK(fmtExact(141) == "141");
  CHECK(fmtExact(86412) == "86,412");
  CHECK(fmtExact(17029868) == "17,029,868");
}

TEST(fmt_rel_time) {
  const int64_t now = 1000000000000LL;
  CHECK(relTime(now - 30000, now) == "刚刚");                    // 30s 前
  CHECK(relTime(now - 3 * 60000LL, now) == "3 分钟前");
  CHECK(relTime(now - 47 * 60000LL, now) == "47 分钟前");
  CHECK(relTime(now - 380 * 60000LL, now) == "6 小时前");
  CHECK(relTime(now - 4300 * 60000LL, now) == "2 天前");
  CHECK(relTime(now + 60000, now) == "刚刚");                    // 未来/乱序钳制
  CHECK(relTime(now - 45 * 1000LL, now) == "刚刚");
  CHECK(relTime(now - 59 * 1000LL, now) == "刚刚");
  CHECK(relTime(now - 60 * 1000LL, now) == "1 分钟前");
}

TEST(fmt_yi_units) {
  CHECK(fmtYi(0) == "0");
  CHECK(fmtYi(999) == "999");
  CHECK(fmtYi(9999) == "9999");
  CHECK(fmtYi(10000) == "1 万");
  CHECK(fmtYi(906288) == "90.6288 万");
  CHECK(fmtYi(2103434288) == "21.03434288 亿");
  CHECK(fmtYi(318345935) == "3.18345935 亿");
}
