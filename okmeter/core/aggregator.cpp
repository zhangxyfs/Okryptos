#include "aggregator.h"
#include <algorithm>
#include <cctype>
#include <ctime>
#include <utility>

namespace okmeter {
namespace {

json::Value sumsJson(const Sums& s) {
  json::Object o;
  o["io"] = json::num(s.inputOther);
  o["icr"] = json::num(s.inputCacheRead);
  o["icc"] = json::num(s.inputCacheCreation);
  o["o"] = json::num(s.output);
  json::Value v;
  v.v = std::move(o);
  return v;
}

void sumsFrom(const json::Value* v, Sums& s) {
  if (!v || !v->isObject()) return;
  if (const json::Value* f = v->find("io")) s.inputOther = (int64_t)f->num();
  if (const json::Value* f = v->find("icr")) s.inputCacheRead = (int64_t)f->num();
  if (const json::Value* f = v->find("icc")) s.inputCacheCreation = (int64_t)f->num();
  if (const json::Value* f = v->find("o")) s.output = (int64_t)f->num();
}

// 损坏的非数字 dayKey 不让 stoi 抛 std::invalid_argument
auto dayKeyOf = [](const std::string& k, int& out) {
  if (k.size() != 8) return false;
  for (char c : k) if (!isdigit((unsigned char)c)) return false;
  out = std::stoi(k);
  return true;
};

template <typename Map>
json::Value mapJson(const Map& m) {
  json::Object o;
  for (const auto& [k, s] : m) o[std::to_string(k)] = sumsJson(s);
  json::Value v;
  v.v = std::move(o);
  return v;
}

json::Value strMapJson(const std::map<std::string, Sums>& m) {
  json::Object o;
  for (const auto& [k, s] : m) o[k] = sumsJson(s);
  json::Value v;
  v.v = std::move(o);
  return v;
}

int64_t dayKeyToMs(int key) {
  std::tm t{};
  t.tm_year = key / 10000 - 1900;
  t.tm_mon = (key / 100) % 100 - 1;
  t.tm_mday = key % 100;
  t.tm_hour = 12;  // 正午，避开 DST 切换边缘
  return (int64_t)std::mktime(&t) * 1000;
}

} // namespace

int Aggregator::dayKey(int64_t ms) {
  time_t t = (time_t)(ms / 1000);
  std::tm lt{};
  localtime_s(&lt, &t);
  return (lt.tm_year + 1900) * 10000 + (lt.tm_mon + 1) * 100 + lt.tm_mday;
}

int Aggregator::weekStartKey(int64_t ms) {
  time_t t = (time_t)(ms / 1000);
  std::tm lt{};
  localtime_s(&lt, &t);
  int back = (lt.tm_wday + 6) % 7;  // 周一起
  lt.tm_mday -= back;
  lt.tm_hour = 12;
  time_t monday = std::mktime(&lt);  // mktime 规范化负 mday
  std::tm mlt{};
  localtime_s(&mlt, &monday);
  return (mlt.tm_year + 1900) * 10000 + (mlt.tm_mon + 1) * 100 + mlt.tm_mday;
}

void Aggregator::add(const UsageEvent& e) {
  const Sums s = Sums::of(e);
  allAll_.plus(s);
  dayAll_[dayKey(e.timeMs)].plus(s);
  sessAll_[e.sessionId].plus(s);
  ModelStat& m = models_[e.model];
  m.all.plus(s);
  m.byDay[dayKey(e.timeMs)].plus(s);
  m.bySession[e.sessionId].plus(s);
  if (e.timeMs > m.lastCallMs) m.lastCallMs = e.timeMs;
  if (e.timeMs >= hotSessionMs_) {  // >=：同毫秒后到的覆盖，保证"最近活跃"单调
    hotSessionMs_ = e.timeMs;
    hotSession_ = e.sessionId;
  }
}

Sums Aggregator::today(int64_t nowMs) const {
  auto it = dayAll_.find(dayKey(nowMs));
  return it == dayAll_.end() ? Sums{} : it->second;
}

Sums Aggregator::week(int64_t nowMs) const {
  const int ws = weekStartKey(nowMs);
  Sums out;
  for (const auto& [k, s] : dayAll_)
    if (weekStartKey(dayKeyToMs(k)) == ws) out.plus(s);
  return out;
}

Sums Aggregator::session() const {
  auto it = sessAll_.find(hotSession_);
  return it == sessAll_.end() ? Sums{} : it->second;
}

std::vector<std::string> Aggregator::modelsByRecency() const {
  std::vector<std::string> out;
  std::vector<std::pair<int64_t, std::string>> keyed;
  for (const auto& [id, m] : models_) keyed.emplace_back(m.lastCallMs, id);
  std::sort(keyed.begin(), keyed.end(),
            [](const auto& a, const auto& b) { return a.first > b.first; });
  for (auto& [_, id] : keyed) out.push_back(id);
  return out;
}

Sums Aggregator::modelToday(const std::string& id, int64_t nowMs) const {
  const ModelStat* m = model(id);
  if (!m) return Sums{};
  auto it = m->byDay.find(dayKey(nowMs));
  return it == m->byDay.end() ? Sums{} : it->second;
}

Sums Aggregator::modelWeek(const std::string& id, int64_t nowMs) const {
  const ModelStat* m = model(id);
  if (!m) return Sums{};
  const int ws = weekStartKey(nowMs);
  Sums out;
  for (const auto& [k, s] : m->byDay)
    if (weekStartKey(dayKeyToMs(k)) == ws) out.plus(s);
  return out;
}

Sums Aggregator::modelMonth(const std::string& id, int64_t nowMs) const {
  const ModelStat* m = model(id);
  if (!m) return Sums{};
  const int ym = dayKey(nowMs) / 100;  // dayKey=yyyymmdd → 月键 yyyymm
  Sums out;
  for (const auto& [k, s] : m->byDay)
    if (k / 100 == ym) out.plus(s);
  return out;
}

Sums Aggregator::modelSession(const std::string& id) const {
  const ModelStat* m = model(id);
  if (!m) return Sums{};
  auto it = m->bySession.find(hotSession_);
  return it == m->bySession.end() ? Sums{} : it->second;
}

json::Value Aggregator::toJson() const {
  json::Object root;
  root["all"] = sumsJson(allAll_);
  root["days"] = mapJson(dayAll_);
  root["sessions"] = strMapJson(sessAll_);
  root["hot"] = json::str(hotSession_);
  root["hotMs"] = json::num(hotSessionMs_);
  json::Object models;
  for (const auto& [id, m] : models_) {
    json::Object mo;
    mo["all"] = sumsJson(m.all);
    mo["days"] = mapJson(m.byDay);
    mo["sessions"] = strMapJson(m.bySession);
    mo["last"] = json::num(m.lastCallMs);
    json::Value mv;
    mv.v = std::move(mo);
    models[id] = std::move(mv);
  }
  json::Value msv;
  msv.v = std::move(models);
  root["models"] = std::move(msv);
  json::Value v;
  v.v = std::move(root);
  return v;
}

bool Aggregator::fromJson(const json::Value& v) {
  if (!v.isObject()) return false;
  sumsFrom(v.find("all"), allAll_);
  if (const json::Value* d = v.find("days"); d && d->isObject())
    for (const auto& [k, s] : d->obj()) {
      int key;
      if (dayKeyOf(k, key)) sumsFrom(&s, dayAll_[key]);
    }
  if (const json::Value* d = v.find("sessions"); d && d->isObject())
    for (const auto& [k, s] : d->obj())
      sumsFrom(&s, sessAll_[k]);
  if (const json::Value* d = v.find("hot")) hotSession_ = d->str();
  if (const json::Value* d = v.find("hotMs")) hotSessionMs_ = (int64_t)d->num(-1);
  if (const json::Value* d = v.find("models"); d && d->isObject())
    for (const auto& [id, mv] : d->obj()) {
      ModelStat m;
      sumsFrom(mv.find("all"), m.all);
      if (const json::Value* md = mv.find("days"); md && md->isObject())
        for (const auto& [k, s] : md->obj()) {
          int key;
          if (dayKeyOf(k, key)) sumsFrom(&s, m.byDay[key]);
        }
      if (const json::Value* md = mv.find("sessions"); md && md->isObject())
        for (const auto& [k, s] : md->obj())
          sumsFrom(&s, m.bySession[k]);
      if (const json::Value* md = mv.find("last")) m.lastCallMs = (int64_t)md->num();
      models_[id] = std::move(m);
    }
  return true;
}

} // namespace okmeter
