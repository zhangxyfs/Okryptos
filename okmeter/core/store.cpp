#include "store.h"
#include <fstream>

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

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
  std::error_code ec;
  std::filesystem::create_directories(dir_, ec);
  json::Object root;
  json::Object cur;
  for (const auto& [k, off] : cursors_) cur[k] = json::num(off);
  json::Value cv;
  cv.v = std::move(cur);
  root["cursors"] = std::move(cv);
  root["agg"] = agg_.toJson();
  json::Value rv;
  rv.v = std::move(root);

  const auto tmp = dir_ / "state.json.tmp";
  {
    std::ofstream out(tmp, std::ios::binary | std::ios::trunc);
    if (!out) return false;
    out << json::dump(rv);
    out.flush();
    if (!out) return false;
  }
  const auto dst = dir_ / "state.json";
  return MoveFileExW(tmp.c_str(), dst.c_str(),
                     MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH) != 0;
}

} // namespace okmeter
