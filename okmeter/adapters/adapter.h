// adapters/adapter.h —— 采集适配器接口：每种 agent 工具一个实现
#pragma once
#include "../core/event.h"
#include <functional>
#include <string>

namespace okmeter {

using EventSink = std::function<void(const UsageEvent&)>;

class IAdapter {
public:
  virtual ~IAdapter() = default;
  virtual std::string id() const = 0;            // 例 "kimi-code"
  virtual int poll(const EventSink& sink) = 0;   // 全量/增量扫描，返回本轮产出事件数
};

} // namespace okmeter
