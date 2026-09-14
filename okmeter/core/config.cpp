#include "config.h"
#include "atomic.h"
#include "minjson.h"
#include <fstream>

namespace okmeter {
namespace {

bool isValidCount(int n) { return n == 1 || n == 3 || n == 5 || n == 7; }

} // namespace

void Config::normalize() {
  if (form != "arc" && form != "capsule" && form != "compass" &&
      form != "level" && form != "wave" && form != "nixie")
    form = "arc";
  if (material != "dark" && material != "frost" &&
      material != "liquid" && material != "glow")
    material = "dark";
  if (!isValidCount(count)) count = 3;
  if (edge != "right" && edge != "left" && edge != "top" && edge != "bottom")
    edge = "right";
  if (expandTrigger != "hover" && expandTrigger != "click") expandTrigger = "hover";
  mapping.resize(count, "auto");
  for (auto& m : mapping) if (m.empty()) m = "auto";
}

bool loadConfig(const std::filesystem::path& dir, Config& out) {
  out = Config{};
  std::ifstream in(dir / "config.json", std::ios::binary);
  if (!in) return false;
  std::string text((std::istreambuf_iterator<char>(in)), std::istreambuf_iterator<char>());
  json::Value v;
  if (!json::parse(text, v) || !v.isObject()) return false;
  if (const json::Value* x = v.find("form")) out.form = x->str();
  if (const json::Value* x = v.find("material")) out.material = x->str();
  if (const json::Value* x = v.find("count")) out.count = (int)x->num(3);
  if (const json::Value* x = v.find("edge")) out.edge = x->str();
  if (const json::Value* x = v.find("mergeCache")) out.mergeCache = x->boolean(true);
  if (const json::Value* x = v.find("pinned")) out.pinned = x->boolean(false);
  if (const json::Value* x = v.find("expandTrigger")) out.expandTrigger = x->str();
  if (const json::Value* m = v.find("mapping"); m && m->isArray()) {
    out.mapping.clear();
    for (const auto& val : m->arr()) out.mapping.push_back(val.str());
  }
  out.normalize();
  return true;
}

bool saveConfig(const std::filesystem::path& dir, const Config& cfg) {
  Config c = cfg;
  c.normalize();
  json::Object root;
  root["form"] = json::str(c.form);
  root["material"] = json::str(c.material);
  root["count"] = json::num(c.count);
  root["edge"] = json::str(c.edge);
  root["mergeCache"] = json::num(c.mergeCache ? 1 : 0);
  root["pinned"] = json::num(c.pinned ? 1 : 0);
  root["expandTrigger"] = json::str(c.expandTrigger);
  json::Array arr;
  for (const auto& m : c.mapping) arr.push_back(json::str(m));
  json::Value av;
  av.v = std::move(arr);
  root["mapping"] = std::move(av);
  json::Value rv;
  rv.v = std::move(root);
  return atomicWriteText(dir / "config.json", json::dump(rv));
}

} // namespace okmeter
