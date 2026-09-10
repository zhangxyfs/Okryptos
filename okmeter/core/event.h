// core/event.h —— 统一 usage 事件（采集适配器 → 核心框架的唯一数据契约）
#pragma once
#include <cstdint>
#include <string>

namespace okmeter {

struct UsageEvent {
  std::string model;       // 例 "kimi-code/k3-256k"
  std::string agentId;     // main / agent-N
  std::string sessionId;   // 来源会话（由适配器从落盘路径提取）
  int64_t inputOther = 0;
  int64_t inputCacheRead = 0;
  int64_t inputCacheCreation = 0;
  int64_t output = 0;
  int64_t timeMs = 0;      // epoch 毫秒
  std::string usageScope;  // 原始 scope（turn 等），保留不解析

  int64_t total() const { return inputOther + inputCacheRead + inputCacheCreation + output; }
};

} // namespace okmeter
