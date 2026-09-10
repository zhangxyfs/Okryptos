// tests/main.cpp —— 单测 runner：逐条跑静态注册的用例
#include "framework.h"

int main() {
  for (auto& c : okmeter::test::cases()) {
    int before = okmeter::test::failures();
    c.fn();
    std::printf("%s %s\n", okmeter::test::failures() == before ? "ok  " : "FAIL", c.name);
  }
  std::printf("%zu cases, %d failures\n",
              okmeter::test::cases().size(), okmeter::test::failures());
  return okmeter::test::failures() ? 1 : 0;
}
