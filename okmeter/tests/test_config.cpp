#include "../core/config.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path tempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

TEST(config_defaults_and_normalize) {
  Config c;
  CHECK(c.form == "arc");
  CHECK(c.material == "dark");
  CHECK(c.count == 3);
  CHECK(c.edge == "right");
  CHECK(c.mergeCache);
  c.count = 4;              // 偶数：钳回奇数
  c.edge = "diag";          // 非法边：回退 right
  c.form = "unknown";
  c.expandTrigger = "dbl";  // 非法触发方式：回退 hover
  c.normalize();
  CHECK(c.count == 3);
  CHECK(c.edge == "right");
  CHECK(c.form == "arc");
  CHECK(c.expandTrigger == "hover");
  CHECK(Config{}.expandTrigger == "hover");  // 缺省悬停
  c.expandTrigger = "click";
  c.normalize();
  CHECK(c.expandTrigger == "click");         // 合法保留
  c.edge = "top";           // 横向边：合法保留
  c.normalize();
  CHECK(c.edge == "top");
  c.edge = "bottom";
  c.normalize();
  CHECK(c.edge == "bottom");
  c.count = 7;
  c.mapping = {"auto", "total:today"};
  c.normalize();
  CHECK(c.count == 7);
  CHECK_EQ(c.mapping.size(), (size_t)7);        // 长度对齐 count，填 auto
  CHECK(c.mapping[1] == "total:today");
  CHECK(c.mapping[6] == "auto");
}

TEST(config_roundtrip) {
  auto d = tempDir("config");
  Config c;
  c.count = 5;
  c.material = "liquid";
  c.edge = "left";
  c.mergeCache = false;
  c.pinned = true;
  c.mapping = {"auto", "model:kimi-code/k3", "total:all", "auto", "auto"};
  c.expandTrigger = "click";
  CHECK(saveConfig(d, c));
  Config back;
  CHECK(loadConfig(d, back));
  CHECK(back.count == 5);
  CHECK(back.material == "liquid");
  CHECK(back.edge == "left");
  CHECK(!back.mergeCache);
  CHECK(back.pinned);
  CHECK(back.expandTrigger == "click");
  CHECK_EQ(back.mapping.size(), (size_t)5);
  CHECK(back.mapping[1] == "model:kimi-code/k3");
  c.edge = "bottom";
  CHECK(saveConfig(d, c));
  CHECK(loadConfig(d, back));
  CHECK(back.edge == "bottom");
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(config_mergecache_bool_literal) {
  auto d = tempDir("config-mergecache");
  {
    std::ofstream f(d / "config.json", std::ios::binary);
    f << "{\"mergeCache\": false}";   // 手写布尔字面量（saveConfig 写的是 0/1 数字）
  }
  Config c;
  CHECK(loadConfig(d, c));
  CHECK(!c.mergeCache);              // 布尔 false 不被吞
  {
    std::ofstream f(d / "config.json", std::ios::binary);
    f << "{\"mergeCache\": 0}";      // 数字 0 兼容
  }
  CHECK(loadConfig(d, c));
  CHECK(!c.mergeCache);
  {
    std::ofstream f(d / "config.json", std::ios::binary);
    f << "{\"mergeCache\": true}";
  }
  CHECK(loadConfig(d, c));
  CHECK(c.mergeCache);
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(config_missing_or_broken_returns_defaults) {
  auto d = tempDir("config-broken");
  Config c;
  c.count = 7;
  CHECK(!loadConfig(d, c));   // 文件缺失 → false 且回默认
  CHECK(c.count == 3);
  {
    std::ofstream f(d / "config.json", std::ios::binary);
    f << "{broken";
  }
  Config c2;
  c2.count = 7;
  CHECK(!loadConfig(d, c2));
  CHECK(c2.count == 3);
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}
