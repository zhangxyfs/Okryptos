#include "../core/isotime.h"
#include "framework.h"

using namespace okmeter;

TEST(isotime_parse) {
  // 2026-08-12T10:44:13.573Z 与显式 +00:00 必须同值
  const int64_t z = isoToMs("2026-08-12T10:44:13.573Z");
  CHECK(z > 0);
  CHECK_EQ(z, isoToMs("2026-08-12T10:44:13.573+00:00"));
  CHECK_EQ(z, isoToMs("2026-08-12T10:44:13.573"));
  // +08:00 偏移：本地 18:44 = UTC 10:44
  CHECK_EQ(z, isoToMs("2026-08-12T18:44:13.573+08:00"));
  // 毫秒截断/补齐
  CHECK_EQ(isoToMs("2026-08-12T10:44:13.5Z"), z - 73);   // .5 → 500ms
  CHECK_EQ(isoToMs("2026-08-12T10:44:13Z"), z - 573);
  // 坏输入
  CHECK_EQ(isoToMs(""), 0);
  CHECK_EQ(isoToMs("not a time"), 0);
}
