// core/minjson.h —— 极简 JSON：仅支撑 wire.jsonl 行解析与 state.json 读写，零依赖
#pragma once
#include <cstdint>
#include <map>
#include <string>
#include <variant>
#include <vector>

namespace okmeter::json {

struct Value;
using Object = std::map<std::string, Value>;
using Array = std::vector<Value>;

struct Value {
  std::variant<std::nullptr_t, bool, double, std::string, Array, Object> v{nullptr};

  bool isObject() const { return std::holds_alternative<Object>(v); }
  bool isArray() const { return std::holds_alternative<Array>(v); }

  const Object& obj() const {
    static const Object kEmpty;
    return isObject() ? std::get<Object>(v) : kEmpty;
  }
  const Array& arr() const {
    static const Array kEmpty;
    return isArray() ? std::get<Array>(v) : kEmpty;
  }
  double num(double dflt = 0) const {
    return std::holds_alternative<double>(v) ? std::get<double>(v) : dflt;
  }
  const std::string& str() const {
    static const std::string kEmpty;
    return std::holds_alternative<std::string>(v) ? std::get<std::string>(v) : kEmpty;
  }
  // 对象查找；非对象或缺键返回 nullptr
  const Value* find(const std::string& key) const {
    if (!isObject()) return nullptr;
    const Object& o = std::get<Object>(v);
    auto it = o.find(key);
    return it == o.end() ? nullptr : &it->second;
  }
};

bool parse(const std::string& text, Value& out);  // 失败返回 false（调用方跳过该行）
std::string dump(const Value& v);                 // 序列化（state.json 写盘用）

inline Value num(int64_t n) { Value v; v.v = (double)n; return v; }
inline Value str(const std::string& s) { Value v; v.v = s; return v; }

} // namespace okmeter::json
