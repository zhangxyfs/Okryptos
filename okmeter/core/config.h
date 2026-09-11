// core/config.h —— 配置模型与 config.json 持久化
#pragma once
#include <filesystem>
#include <string>
#include <vector>

namespace okmeter {

struct Config {
  std::string form = "arc";        // arc / capsule / compass
  std::string material = "dark";   // dark / frost / liquid / glow
  int count = 3;                   // 仅奇数 1/3/5/7
  std::string edge = "right";      // right / left（v1 不做顶底）
  bool mergeCache = true;
  bool pinned = false;             // 保持显示：不自动隐藏（迟滞/离开收回均禁用）
  std::vector<std::string> mapping;  // "auto" | "total:session|today|week|all" | "model:<id>"

  void normalize();  // 非法值回退默认；count 钳奇数集；mapping 长度对齐 count
};

bool loadConfig(const std::filesystem::path& dir, Config& out);  // 缺/坏 → false 且 out=默认
bool saveConfig(const std::filesystem::path& dir, const Config& cfg);

} // namespace okmeter
