// tests/framework.h —— 微型断言框架：静态注册 + 失败计数，零依赖
#pragma once
#include <cstdio>
#include <vector>

namespace okmeter::test {

struct Case { const char* name; void (*fn)(); };

inline std::vector<Case>& cases() { static std::vector<Case> c; return c; }
inline int& failures() { static int f = 0; return f; }

struct Auto {
  Auto(const char* n, void (*f)()) { cases().push_back({n, f}); }
};

} // namespace okmeter::test

#define TEST(name) \
  static void name(); \
  static ::okmeter::test::Auto auto_##name(#name, name); \
  static void name()

#define CHECK(cond) do { if (!(cond)) { \
  std::printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond); \
  ++::okmeter::test::failures(); } } while (0)

#define CHECK_EQ(a, b) do { auto _va = (a); auto _vb = (b); if (!(_va == _vb)) { \
  std::printf("FAIL %s:%d: %s == %s\n", __FILE__, __LINE__, #a, #b); \
  ++::okmeter::test::failures(); } } while (0)
