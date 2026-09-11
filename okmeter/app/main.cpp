// app/main.cpp —— 入口：默认启动 dock（Plan 2 起为产品形态）；
// --scan 保留 Plan 1 控制台冒烟（扫描真实 home 打印各口径总量）
#include "../adapters/kimi/adapter.h"
#include "../core/aggregator.h"
#include "../core/paths.h"
#include "../core/store.h"
#include "../ui/app.h"
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <string>

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

using namespace okmeter;

static int64_t nowMs() {
  return std::chrono::duration_cast<std::chrono::milliseconds>(
      std::chrono::system_clock::now().time_since_epoch()).count();
}

static void printSums(const char* label, const Sums& s) {
  std::printf("%-8s %lld tokens (in %lld / cache-read %lld / cache-create %lld / out %lld)\n",
              label, (long long)s.total(), (long long)s.inputOther,
              (long long)s.inputCacheRead, (long long)s.inputCacheCreation,
              (long long)s.output);
}

static int runSmoke() {
  SetConsoleOutputCP(CP_UTF8);
  Store store(okmeterDir());
  store.load();
  KimiAdapter kimi(kimiHome(), &store);
  Aggregator& agg = store.agg();
  const int n = kimi.poll([&](const UsageEvent& e) { agg.add(e); });
  store.flush();

  std::printf("kimi-home: %s\n+ %d new usage records\n\n",
              pathU8(kimiHome()).c_str(), n);
  const int64_t now = nowMs();
  printSums("session", agg.session());
  printSums("today", agg.today(now));
  printSums("week", agg.week(now));
  printSums("all", agg.all());
  std::printf("\nmodels by recency:\n");
  for (const std::string& id : agg.modelsByRecency()) {
    const ModelStat* m = agg.model(id);
    std::printf("  %-24s all %lld\n", id.c_str(), (long long)m->all.total());
  }
  return 0;
}

static std::wstring argWide(const char* s) {
  const int n = MultiByteToWideChar(CP_UTF8, 0, s, -1, nullptr, 0);
  std::wstring p(n > 0 ? (size_t)n - 1 : 0, L'\0');
  if (n > 0) MultiByteToWideChar(CP_UTF8, 0, s, -1, p.data(), n);
  return p;
}

int main(int argc, char** argv) {
  if (argc > 1 && std::string(argv[1]) == "--scan") return runSmoke();
  DockApp app;
  if (argc > 2 && std::string(argv[1]) == "--shot")
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]));
  // --shotmenu <png> [slot]：自检截图前打开自绘菜单（slot -1=空白菜单，缺省中心球）
  if (argc > 2 && std::string(argv[1]) == "--shotmenu") {
    const int slot = argc > 3 ? std::atoi(argv[3]) : -1;
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]), slot);
  }
  // --shotsettings <png> [dropSlot]：自检截图前打开背板设置面板（dropSlot ≥0 时
  // 同时打开该槽位的指标下拉浮层）
  if (argc > 2 && std::string(argv[1]) == "--shotsettings") {
    const int dropSlot = argc > 3 ? std::atoi(argv[3]) : -1;
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]), -2, true, dropSlot);
  }
  return app.run(GetModuleHandleW(nullptr));
}
