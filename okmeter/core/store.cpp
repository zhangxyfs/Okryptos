#include "store.h"
#include "atomic.h"
#include <fstream>

namespace okmeter {

bool Store::load() {
  std::ifstream in(dir_ / "state.json", std::ios::binary);
  if (!in) return false;
  std::string text((std::istreambuf_iterator<char>(in)), std::istreambuf_iterator<char>());
  json::Value v;
  if (!json::parse(text, v) || !v.isObject()) return false;
  if (const json::Value* c = v.find("cursors"))
    for (const auto& [k, off] : c->obj())
      cursors_[k] = (int64_t)off.num();
  if (const json::Value* a = v.find("agg")) agg_.fromJson(*a);
  return true;
}

bool Store::flush() {
  json::Object root;
  json::Object cur;
  for (const auto& [k, off] : cursors_) cur[k] = json::num(off);
  json::Value cv;
  cv.v = std::move(cur);
  root["cursors"] = std::move(cv);
  root["agg"] = agg_.toJson();
  json::Value rv;
  rv.v = std::move(root);
  return atomicWriteText(dir_ / "state.json", json::dump(rv));
}

} // namespace okmeter
