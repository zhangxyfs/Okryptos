// render/forms/nixie.cpp —— 辉光数码形态：106px 圆角块直线排布（step 96），
// 七段数码管显示紧凑值（15×26 字模，段 3px；小数点 4px 宽），下挂单位与模型名。
// 中心项 accent 数码；块底委托材质（胶囊分支）。
#include "../form.h"
#include "../material.h"
#include <algorithm>
#include <cmath>
#include <cstdio>
#include <cstring>
#include <string>

namespace okmeter::render {
namespace {

constexpr UINT32 kAccent = 0x5FE0A8;
constexpr double kStep = 96;     // 原型 geom() nixie：step 96
constexpr double kW = 128;       // 原型 W=128
constexpr float kItemHalfW = 53.0f;   // 块半宽（106/2）
constexpr float kItemHalfH = 30.0f;   // 块半高（padding 11+数码 26+间距 4+名 10+padding 9）

// 七段字模（原型 SEG 表）：a 顶 b 右上 c 右下 d 底 e 左下 f 左上 g 中
const char* kSegs[11] = {
  "abcdef", "bc", "abdeg", "abcdg", "bcfg",
  "acdfg", "acdefg", "abc", "abcdefg", "abcdfg", "p"  // 0-9 + 小数点
};

void drawDigit(ID2D1DeviceContext* dc, ID2D1SolidColorBrush* brush, char ch,
               float x, float y, float u, D2D1_COLOR_F lit, D2D1_COLOR_F unlit) {
  // 字模 15×26；横段厚 3 长 11，纵段厚 3 长 10（原型 .dg 同款定位）
  int idx = ch >= '0' && ch <= '9' ? ch - '0' : (ch == '.' ? 10 : -1);
  if (idx < 0) return;
  const char* segs = kSegs[idx];
  const float T = 3.0f * u;   // 段厚 ×比例
  const float H = 10.0f * u;  // 纵段长 ×比例
  auto seg = [&](char s, D2D1_RECT_F r) {
    const bool on = strchr(segs, s) != nullptr;
    brush->SetColor(on ? lit : unlit);
    const float rr = s == 'p' ? 1.5f : 1.0f;
    if (s == 'p')
      dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F((r.left + r.right) * 0.5f,
                                                  (r.top + r.bottom) * 0.5f),
                                    (r.right - r.left) * 0.5f,
                                    (r.bottom - r.top) * 0.5f), brush);
    else
      dc->FillRoundedRectangle(D2D1::RoundedRect(r, rr, rr), brush);
  };
  if (ch == '.') {
    seg('p', D2D1::RectF(x + 0.5f * u, y + 23.0f * u, x + 3.5f * u, y + 26.0f * u));
    return;
  }
  seg('a', D2D1::RectF(x + 2 * u, y, x + 13 * u, y + T));                 // 顶横
  seg('g', D2D1::RectF(x + 2 * u, y + 11.5f * u, x + 13 * u, y + 14.5f * u));  // 中横
  seg('d', D2D1::RectF(x + 2 * u, y + 23 * u, x + 13 * u, y + 26 * u));   // 底横
  seg('f', D2D1::RectF(x, y + 2 * u, x + T, y + 2 * u + H));              // 左上
  seg('b', D2D1::RectF(x + 12 * u, y + 2 * u, x + 15 * u, y + 2 * u + H));  // 右上
  seg('e', D2D1::RectF(x, y + 14 * u, x + T, y + 14 * u + H));            // 左下
  seg('c', D2D1::RectF(x + 12 * u, y + 14 * u, x + 15 * u, y + 14 * u + H));  // 右下
}

class NixieForm final : public IForm {
public:
  std::string id() const override { return "nixie"; }

  DockGeom layout(int n, int screenH, const std::string& edge) const override {
    DockGeom g;
    if (n < 1) return g;
    const int mid = (n - 1) / 2;
    const double s = uiScale(screenH);  // 比例法：原型值 × 屏高/1080
    g.scale = s;
    g.w = kW * s;
    g.h = 2 * (mid * kStep * s + 68 * s);
    g.connector = false;  // 原型 nixie 隐藏 arcSvg
    g.items.resize((size_t)n);
    for (int i = 0; i < n; ++i) {
      g.items[(size_t)i].x = g.w / 2;
      g.items[(size_t)i].y = g.h / 2 + (i - mid) * kStep * s;
      g.items[(size_t)i].r = (float)(kItemHalfH * s);
      g.items[(size_t)i].hw = (float)(kItemHalfW * s);
    }
    (void)edge;
    return g;
  }

  double hoverPush() const override { return 0; }
  double cardRadius(int, int) const override { return 20; }  // 卡片部分压条目（用户裁决，对齐 wave/level 观感；原型 RADII nixie=54 不压）

  void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const override {
    if (!dc || !ctx.geom || !ctx.items || !ctx.material || !ctx.brush ||
        !ctx.labelFmt) return;
    const DockGeom& g = *ctx.geom;
    const size_t n = g.items.size();
    const double e = ctx.e < 0 ? 0 : ctx.e > 1 ? 1 : ctx.e;
    const float dimBase = (float)(0.55 + 0.45 * e);  // 收缩态 55%（原型同款）
    bool anyHot = false;  // 有悬停项时非悬停项降暗（原型 .dim）
    for (const ItemGeom& it : g.items) anyHot = anyHot || it.scale > 1.001;

    for (size_t i = 0; i < n; ++i) {
      const ItemGeom& it = g.items[i];
      const D2D1_POINT_2F c{ (float)it.x, (float)it.y };
      const bool isHot = it.scale > 1.001;
      const float dim = dimBase * ((anyHot && !isHot) ? (float)hoverDimOpacity() : 1.0f);  // 原型 .dim 降暗

      OrbStyleCtx osc{};
      osc.d3d = ctx.d3d;
      osc.backdrop = ctx.backdrop;
      osc.brush = ctx.brush;
      osc.center = c;
      osc.r = (float)(it.r * it.scale);
      osc.halfW = (float)(it.hw * it.scale);
      osc.isCenter = (int)i == ctx.mid;
      osc.isHot = isHot;
      osc.dimmed = dim;
      osc.backdropDX = ctx.backdropDX;
      osc.backdropDY = ctx.backdropDY;
      osc.cornerR = 12.0f * (float)g.scale;  // 块状角半径 ×比例
      if (isHot)
        dc->SetTransform(D2D1::Matrix3x2F::Scale((float)it.scale, (float)it.scale, c));
      ctx.material->drawOrbBack(dc, osc);
      dc->SetTransform(D2D1::IdentityMatrix());

      if (i >= ctx.items->size()) continue;
      const DockItem& di = (*ctx.items)[i];
      // 紧凑值拆分（原型 fmtSplit）：数字串 + 单位
      std::string digits;
      std::wstring unit;
      const double v = di.raw;
      if (v < 1000.0) {
        digits = std::to_string((int)std::llround(v));
      } else if (v < 1e6) {
        const double x = v / 1e3;
        digits = x < 100 ? fmt1(x) : std::to_string((int)std::llround(x));
        unit = L"K";
      } else {
        const double x = v / 1e6;
        digits = x < 100 ? fmt1(x) : std::to_string((int)std::llround(x));
        unit = L"M";
      }
      // 数码行居中：总宽 = 数字 15·N + 间隔 3·(N-1)（点号 4 宽）+ 单位 ~8（×比例）
      const float u = (float)g.scale;
      float totalW = 0;
      for (char ch : digits) totalW += (ch == '.' ? 4.0f + 3.0f : 15.0f + 3.0f) * u;
      if (!digits.empty()) totalW -= 3.0f * u;
      if (!unit.empty()) totalW += (4.0f + 8.0f) * u;
      float x = c.x - totalW * 0.5f;
      const float y = c.y - 19.0f * u;
      const D2D1_COLOR_F lit = osc.isCenter
          ? D2D1::ColorF(kAccent, dim) : inkLight( 0.5f * dim);
      const D2D1_COLOR_F unlit = inkLight( 0.5f * 0.13f * dim);
      for (char ch : digits) {
        const float dw = (ch == '.' ? 4.0f : 15.0f) * u;
        drawDigit(dc, ctx.brush, ch, x, y, u, lit, unlit);
        x += dw + 3.0f * u;
      }
      if (!unit.empty()) {
        const D2D1_RECT_F tr = D2D1::RectF(x + 1.0f * u, y + 12.0f * u, x + 12.0f * u, y + 26.0f * u);
        if (ctx.material && ctx.material->id() == "liquid" && !osc.isCenter)
          liquidHalo(dc, ctx.brush, unit, ctx.labelFmt, tr, (float)dim);
        ctx.brush->SetColor(osc.isCenter ? D2D1::ColorF(kAccent, dim)
                                         : inkLight( 0.66f * dim));
        dc->DrawText(unit.c_str(), (UINT32)unit.size(), ctx.labelFmt, &tr, ctx.brush,
                     D2D1_DRAW_TEXT_OPTIONS_NONE, DWRITE_MEASURING_MODE_NATURAL);
      }
      // 模型名（8.5px dim，原型 .nx .k 居中省略）
      if (!di.label.empty()) {
        const D2D1_RECT_F tr = D2D1::RectF(c.x - 48.0f * u, c.y + 11.0f * u, c.x + 48.0f * u, c.y + 23.0f * u);
        if (ctx.material && ctx.material->id() == "liquid")
          liquidHalo(dc, ctx.brush, di.label, ctx.labelFmt, tr, (float)dim);
        ctx.brush->SetColor(inkLight( 0.5f * dim));
        dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), ctx.labelFmt,
                     &tr, ctx.brush);
      }
    }
  }

private:
  static std::string fmt1(double x) {  // 一位小数（去尾 .0）
    char buf[32];
    snprintf(buf, sizeof(buf), "%.1f", x);
    std::string s = buf;
    if (s.size() > 2 && s.compare(s.size() - 2, 2, ".0") == 0) s.resize(s.size() - 2);
    return s;
  }
};

} // namespace

std::unique_ptr<IForm> createNixieForm() {
  return std::unique_ptr<IForm>(new NixieForm());
}

} // namespace okmeter::render
