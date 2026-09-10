// core/config.h —— 配置模型与 config.json 持久化
#pragma once
#include <filesystem>
#include <string>
#include <vector>

namespace okmeter {

struct Config {
  std::string form = "arc";        // arc（v1 渲染）；capsule/compass 属 Plan 2b
  std::string material = "dark";   // dark（v1 渲染）；frost/liquid/glow 属 Plan 2b
  int count = 3;                   // 仅奇数 1/3/5/7
  std::string edge = "right";      // right / left（v1 不做顶底）
  bool mergeCache = true;
  std::vector<std::string> mapping;  // "auto" | "total:session|today|week|all" | "model:<id>"

  void normalize();  // 非法值回退默认；count 钳奇数集；mapping 长度对齐 count
};

bool loadConfig(const std::filesystem::path& dir, Config& out);  // 缺/坏 → false 且 out=默认
bool saveConfig(const std::filesystem::path& dir, const Config& cfg);

} // namespace okmeter
