// core/registry.h —— 通用"名称→工厂"注册表（渲染形态/材质的分发已改走
// render/catalog.h 模块目录，目录元数据与工厂配对同源）
#pragma once
#include <functional>
#include <map>
#include <memory>
#include <string>
#include <vector>

namespace okmeter {

template <typename T>
class Registry {
public:
  using Factory = std::function<std::unique_ptr<T>()>;

  void add(const std::string& name, Factory f) { factories_[name] = std::move(f); }

  std::unique_ptr<T> create(const std::string& name) const {
    auto it = factories_.find(name);
    return it == factories_.end() ? nullptr : it->second();
  }

  std::vector<std::string> names() const {
    std::vector<std::string> out;
    out.reserve(factories_.size());
    for (const auto& [k, _] : factories_) out.push_back(k);
    return out;
  }

private:
  std::map<std::string, Factory> factories_;
};

} // namespace okmeter
