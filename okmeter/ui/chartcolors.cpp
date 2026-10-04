#include "chartcolors.h"
#include <algorithm>
#include <cmath>

namespace okmeter::chartcolors {
namespace {

constexpr float kGolden = 137.5f;  // 黄金角
constexpr float kOS = 65.0f, kOL = 55.0f;  // 溢出提供商固定 S/L

// 策展色表（键 = providerOf 的返回名）：两两 ΔHue≥25°，改表后由
// test_chartcolors 的 pairwise 用例把关
const struct { const char* name; Hsl hsl; } kCurated[] = {
    {"月之暗面", {258.0f, 82.0f, 66.0f}},
    {"OpenAI",   {174.0f, 60.0f, 44.0f}},
    {"DeepSeek", {217.0f, 88.0f, 62.0f}},
    {"通义",     { 28.0f, 92.0f, 58.0f}},
    {"混元",     {348.0f, 78.0f, 62.0f}},
    {"智谱",     { 90.0f, 75.0f, 55.0f}},
    {"Anthropic",{ 55.0f, 85.0f, 58.0f}},
    {"Google",   {140.0f, 70.0f, 50.0f}},
    {"豆包",     {115.0f, 75.0f, 55.0f}},
};

float norm360(float h) {
  h = std::fmod(h, 360.0f);
  return h < 0 ? h + 360.0f : h;
}

} // namespace

float hueDist(float a, float b) {
  const float d = std::fabs(norm360(a) - norm360(b));
  return (std::min)(d, 360.0f - d);
}

bool curated(const std::string& provider, Hsl& out) {
  for (const auto& c : kCurated)
    if (provider == c.name) { out = c.hsl; return true; }
  return false;
}

std::vector<Hsl> providerColors(const std::vector<std::string>& names) {
  std::vector<Hsl> out(names.size());
  std::vector<float> gen;  // 已生成的溢出色相（避让对象）
  int idx = 0;             // 黄金角步进序（含被避让步进的消耗，原型同款）
  for (size_t i = 0; i < names.size(); ++i) {
    if (curated(names[i], out[i])) continue;
    // 一个完整周期（144 步，137.5/360 有理数必回绕）内：首个满足 ΔHue≥25° 的
    // 候选胜出（原型语义）；策展表稠密导致周期内无解时，取色距最大者——
    // 防死循环（9 策展色 ×±25° 覆盖已超 360°，严格规则数学上不可满足）
    float h = 0.0f, best = 0.0f, bestD = -1.0f;
    bool found = false;
    for (int tries = 0; tries < 144 && !found; ++tries) {
      h = norm360(kGolden * (float)idx++);
      float d = 360.0f;
      for (const auto& c : kCurated) d = (std::min)(d, hueDist(h, c.hsl.h));
      for (float g : gen) d = (std::min)(d, hueDist(h, g));
      if (d >= 25.0f) found = true;
      else if (d > bestD) { bestD = d; best = h; }
    }
    if (!found) h = best;
    gen.push_back(h);
    out[i] = Hsl{h, kOS, kOL};
  }
  return out;
}

Hsl drilled(Hsl base, int i, int n) {
  // 提供商色相家族内展开（用户裁决：模型随提供商同色相——"都是紫色，只是不同的
  // 紫"）：色相以族心对称、全族跨度 ≤36°（n 大时均摊小步长，不漂出家族色）；
  // 明度单调斜坡（跨度 ≤30），逐对 hue+L 双重渐变可辨
  const float t = (float)i - (float)(n - 1) * 0.5f;  // 以族心对称
  const float hstep = (std::min)(18.0f, 36.0f / (float)(std::max)(1, n - 1));
  const float lstep = (std::min)(15.0f, 30.0f / (float)(std::max)(1, n - 1));
  Hsl c;
  c.h = norm360(base.h + t * hstep);
  c.s = base.s;
  c.l = (std::max)(12.0f, (std::min)(88.0f, base.l + t * lstep));
  return c;
}

Rgb toRgb(Hsl c) {
  const float h = norm360(c.h) / 60.0f;
  const float s = c.s / 100.0f, l = c.l / 100.0f;
  const float chroma = (1.0f - std::fabs(2.0f * l - 1.0f)) * s;
  const float x = chroma * (1.0f - std::fabs(std::fmod(h, 2.0f) - 1.0f));
  float r = 0, g = 0, b = 0;
  const int seg = (int)h % 6;
  if (seg == 0) { r = chroma; g = x; }
  else if (seg == 1) { r = x; g = chroma; }
  else if (seg == 2) { g = chroma; b = x; }
  else if (seg == 3) { g = x; b = chroma; }
  else if (seg == 4) { r = x; b = chroma; }
  else { r = chroma; b = x; }
  const float m = l - chroma * 0.5f;
  return Rgb{r + m, g + m, b + m};
}

} // namespace okmeter::chartcolors
