// app/main.cpp —— 入口：默认启动 dock（Plan 2 起为产品形态）；
// --scan 保留 Plan 1 控制台冒烟（扫描真实 home 打印各口径总量）
#include "../adapters/claude/adapter.h"
#include "../adapters/codex/adapter.h"
#include "../adapters/kimi/adapter.h"
#include "../adapters/qwen/adapter.h"
#include "../adapters/workbuddy/adapter.h"
#include "../adapters/zcode/adapter.h"
#include "../core/aggregator.h"
#include "../core/paths.h"
#include "../core/store.h"
#include "../ui/app.h"
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <memory>
#include <string>
#include <vector>

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
  Aggregator& agg = store.agg();
  struct Src { const char* id; std::unique_ptr<IAdapter> adapter; };
  std::vector<Src> srcs;
  srcs.push_back({"kimi", std::make_unique<KimiAdapter>(kimiHome(), &store)});
  srcs.push_back({"claude", std::make_unique<ClaudeAdapter>(claudeHome(), &store)});
  srcs.push_back({"codex", std::make_unique<CodexAdapter>(codexHome(), &store)});
  srcs.push_back({"qwen", std::make_unique<QwenAdapter>(qwenHome(), &store)});
  srcs.push_back({"zcode", std::make_unique<ZcodeAdapter>(zcodeHome(), &store)});
  srcs.push_back({"workbuddy", std::make_unique<WorkbuddyAdapter>(workbuddyHome(), &store)});
  int n = 0;
  for (auto& s : srcs) {
    const int c = s.adapter->poll([&](const UsageEvent& e) { agg.add(e); });
    std::printf("%-8s + %d new usage records\n", s.id, c);
    n += c;
  }
  store.flush();

  std::printf("\ntotal + %d\n\n", n);
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
  // 单实例守卫（托盘拉起/双击重复启动静默退出；自检 --shot 系列不受限——
  // 自检实例 2.5s 自毁，不与常驻实例互斥）
  const bool isShot = argc > 2 && std::string(argv[1]).rfind("--shot", 0) == 0;
  HANDLE instMutex = nullptr;
  if (!isShot) {
    instMutex = CreateMutexW(nullptr, TRUE, L"Global\\OkMeter.SingleInstance");
    if (GetLastError() == ERROR_ALREADY_EXISTS) {
      if (instMutex) CloseHandle(instMutex);
      return 0;  // 已有实例：静默退出（托盘重复拉起语义）
    }
  }
  DockApp app;
  if (argc > 2 && std::string(argv[1]) == "--shot")
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]));
  // --shotcap <png>：自检收缩态截图（e=0 露出条，无悬停）
  if (argc > 2 && std::string(argv[1]) == "--shotcap")
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]), -2, false, -1, true);
  // --shotmenu <png> [slot] [sub]：自检截图前打开自绘菜单（slot -1=空白菜单，缺省中心球；
  // sub ≥1 时展开二级（1=总量 2=模型厂商），==2 时同时展开首个厂商的三级）
  if (argc > 2 && std::string(argv[1]) == "--shotmenu") {
    const int slot = argc > 3 ? std::atoi(argv[3]) : -1;
    const int sub = argc > 4 ? std::atoi(argv[4]) : 0;
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]), slot, false, sub);
  }
  // --shotsettings <png> [dropSlot]：自检截图前打开背板设置面板（dropSlot ≥0 时
  // 同时打开该槽位的指标下拉浮层；dropSlot ≥1000 时改为点击球数 chip（值=dropSlot-1000））
  if (argc > 2 && std::string(argv[1]) == "--shotsettings") {
    const int dropSlot = argc > 3 ? std::atoi(argv[3]) : -1;
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]), -2, true, dropSlot);
  }
  // --shotcount <png> <count>：自检——打开设置面板后点球数 chip（走 activateSettings 真实路径）
  if (argc > 3 && std::string(argv[1]) == "--shotcount")
    return app.run(GetModuleHandleW(nullptr), argWide(argv[2]), -2, true,
                   1000 + std::atoi(argv[3]));
  return app.run(GetModuleHandleW(nullptr));
}
