#include "binding.h"

namespace okmeter {
namespace {

// r 名的槽位：最近(r=0)占中心，次近交替向两侧（原型 slotForRank 同款）
int slotForRank(int r, int n) {
  const int mid = (n - 1) / 2;
  if (r == 0) return mid;
  return r % 2 == 1 ? mid - (r + 1) / 2 : mid + r / 2;
}

bool parseScope(const std::string& s, Scope& out) {
  if (s == "session") { out = Scope::Session; return true; }
  if (s == "today")   { out = Scope::Today;   return true; }
  if (s == "week")    { out = Scope::Week;    return true; }
  if (s == "all")     { out = Scope::All;     return true; }
  return false;
}

Binding totalAll() { return Binding{}; }

} // namespace

std::vector<Binding> resolveBindings(const Config& cfg, const Aggregator& agg) {
  Config c = cfg;
  c.normalize();
  const int n = c.count;
  const auto ranked = agg.modelsByRecency();
  std::vector<Binding> out((size_t)n);
  // slotForRank 定义 rank→槽位，反转得槽位→rank；auto 位按槽位 rank 直接索引
  // recency 列表（显式槽位不占 rank 名额，与原型 bySlot 同款）
  std::vector<int> rankOf((size_t)n, -1);
  for (int r = 0; r < n; ++r) rankOf[(size_t)slotForRank(r, n)] = r;
  for (int i = 0; i < n; ++i) {
    const std::string& m = c.mapping[(size_t)i];
    if (m.rfind("total:", 0) == 0) {
      Scope s;
      if (parseScope(m.substr(6), s)) { out[(size_t)i] = Binding{false, s, ""}; continue; }
    }
    if (m.rfind("model:", 0) == 0 && m.size() > 6) {
      out[(size_t)i] = Binding{true, Scope::All, m.substr(6)};
      continue;
    }
    // auto（含非法值回退）：按槽位 rank 取 recency；rank 耗尽 → 全部累计
    const int r = rankOf[(size_t)i];
    if (r >= 0 && r < (int)ranked.size())
      out[(size_t)i] = Binding{true, Scope::All, ranked[(size_t)r]};
    else
      out[(size_t)i] = totalAll();
  }
  return out;
}

} // namespace okmeter
