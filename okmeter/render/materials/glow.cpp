// render/materials/glow.cpp —— 沉浸光感材质（对标鸿蒙沉浸光感三特性）：
// ① 内容自适应毛玻璃：极低底色 + 强模糊 σ13≈blur(26px) + saturate(170%)→0.85
//    + brightness(1.08)，顶部受光（ink 11%→3% 纵向衰减）/底部回落；
// ② 弹性光效反馈：按压扩散环（白芯 36% + accent 18% 径向，0.6s 过冲扩散；
//    球体 scale .9 由 app/scene 烘焙进几何）；
// ③ 粒子汇聚/消散：加法混合（D2D1_PRIMITIVE_BLEND_ADD）——环境微粒常驻悬浮，
//    悬停汇聚到所指项，离开/数据到达向外迸散。
// 柔光层跟指针连续移动汇聚（白芯 120px 16% + accent 边 230px 13% 双层径向，
// 画在球底层 = drawArcStroke 时机）；统一光源：邻近球朝向指针侧边缘亮边
//（方向/距离衰减与 liquid 镜面高光同一套 glassfx::ptrLight）。
// 环境光源在屏缘之外缓慢气态漂移（gasdrift 21s）。wantsTick 常开：粒子/柔光连续动画。
#include "glassfx.h"
#include <chrono>
#include <dxgi.h>
#include <random>

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

double nowSeconds() {
  return std::chrono::duration<double>(
             std::chrono::steady_clock::now().time_since_epoch())
      .count();
}

struct Mote {
  float x = 0, y = 0;    // 基准：ambient=出生点，gather/burst=目标点
  float dx = 0, dy = 0;  // ambient=全程位移，gather=出生偏移（向 0 收敛），burst=迸散位移
  double t0 = 0, dur = 1;
  float maxA = 0.5f;
  int kind = 0;  // 0 环境 1 汇聚 2 迸散
};

struct PressRing { float x = 0, y = 0; double t0 = 0; };

class GlowMaterial final : public IMaterial {
public:
  std::string id() const override { return "glow"; }
  float backdropLuma() const override { return lastLuma_; }

  void onPointer(float x, float y) const override {
    px_ = x;
    py_ = y;
    hasPtr_ = true;
  }
  void onPointerLeave() const override {
    if (hasPtr_) burstAt(gx_, gy_);  // 离开即消散（原型 hideCard → burstAt）
    hasPtr_ = false;
    lit_ = false;
  }
  void onPress(float x, float y) const override {
    rings_.push_back({x, y, nowSeconds()});
    if (rings_.size() > 8) rings_.erase(rings_.begin());
  }
  void onPulse() const override {
    if (hasPtr_) burstAt(gxT_, gyT_);  // 数据到达 → 当前汇聚点迸散
  }
  bool wantsTick() const override { return true; }  // 粒子/柔光/气态漂移常驻动画

  // 球底层（drawItems 之前）：环境气态光 → 汇聚柔光 → 粒子（加法）→ 按压环
  // → 弧线描边。同时承担粒子/柔光状态推进（按真实墙钟 dt）。
  void drawArcStroke(ID2D1DeviceContext* dc, const DockGeom& g,
                     const std::string& edge) const override {
    if (!dc) return;
    const double now = nowSeconds();
    float dt = lastT_ > 0 ? (float)(now - lastT_) : 0.016f;
    dt = std::clamp(dt, 0.0f, 0.05f);
    lastT_ = now;

    // 汇聚目标：指针最近项（原型 hotSlot → slotPos[i]）
    if (hasPtr_ && !g.items.empty()) {
      float best = 1e9f;
      for (const ItemGeom& it : g.items) {
        const float d = std::hypot(px_ - (float)it.x, py_ - (float)it.y);
        if (d < best) {
          best = d;
          gxT_ = (float)it.x;
          gyT_ = (float)it.y;
        }
      }
      lit_ = true;
    }
    if (!litInit_) {  // 初次点亮直接落位，避免从 (0,0) 滑入
      gx_ = gxT_;
      gy_ = gyT_;
      litInit_ = true;
    }
    // 柔光平滑移动/淡入淡出（原型 --gx/.34s ease + opacity .38s transition）
    const float kMove = 1.0f - std::exp(-dt / 0.17f);
    const float kFade = 1.0f - std::exp(-dt / 0.19f);
    gx_ += (gxT_ - gx_) * kMove;
    gy_ += (gyT_ - gy_) * kMove;
    litA_ += ((lit_ ? 1.0f : 0.0f) - litA_) * kFade;

    // 屏缘方向由 app 显式传入（不猜）；光源锚点取球群实际屏缘侧外 170px——
    // 宽窗态（含卡区）g.w 只是球区宽，直接 g.w+170 会把亮心画进窗口内部
    const float side = edge == "left" ? -1.0f : 1.0f;
    float edgeX = side > 0 ? 0.0f : (float)g.w;
    float extent = 30.0f;
    for (const ItemGeom& it : g.items) {  // windows.h min/max 宏冲突，手写比较
      if (side > 0 ? (float)it.x > edgeX : (float)it.x < edgeX)
        edgeX = (float)it.x;
      const float ex = (float)(it.hw > 0 ? it.hw : it.r);
      if (ex > extent) extent = ex;
    }
    edgeX += side * extent;  // 项半径/半宽 → 窗口屏缘
    const float gasX = edgeX + side * 170.0f;
    const float h = (float)g.h;

    ensureBrushes(dc);
    updateMotes(now, dt, g, side);

    // ① 环境气态光（原型 .dock::before：光源在屏缘外，21s 缓慢漂移）
    if (gasInk_ && gasAccent_) {
      const double w = now * 6.28318530718 / 21.0;
      gasInk_->SetCenter(D2D1::Point2F(gasX + (float)std::sin(w) * 14.0f,
                                       h * 0.42f + (float)std::cos(w * 0.7) * 20.0f));
      dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(gasX, h * 0.42f), 340.0f, 480.0f),
                      gasInk_.Get());
      gasAccent_->SetCenter(
          D2D1::Point2F(gasX + side * 40.0f - (float)std::sin(w * 0.8) * 16.0f,
                        h * 0.62f + (float)std::cos(w) * 14.0f));
      dc->FillEllipse(
          D2D1::Ellipse(D2D1::Point2F(gasX + side * 40.0f, h * 0.62f), 300.0f, 430.0f),
          gasAccent_.Get());
    }

    // ② 汇聚柔光（白芯 + accent 边双层径向，layer 不透明度 = litA_ 淡入淡出）
    if (litA_ > 0.01f && litCore_ && litEdge_) {
      const D2D1_RECT_F bounds =
          D2D1::RectF(gx_ - 240.0f, gy_ - 240.0f, gx_ + 240.0f, gy_ + 240.0f);
      dc->PushLayer(D2D1::LayerParameters1(bounds, nullptr,
                                           D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
                                           D2D1::Matrix3x2F::Identity(), litA_),
                    nullptr);
      litCore_->SetCenter(D2D1::Point2F(gx_, gy_));
      dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(gx_, gy_), 120.0f, 120.0f),
                      litCore_.Get());
      litEdge_->SetCenter(D2D1::Point2F(gx_, gy_));
      dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(gx_, gy_), 230.0f, 230.0f),
                      litEdge_.Get());
      dc->PopLayer();
    }

    // ③ 粒子（加法混合：原型 motes 的白芯 + accent 辉光 box-shadow）
    if (!motes_.empty() && ctxBrushReady(dc)) {
      dc->SetPrimitiveBlend(D2D1_PRIMITIVE_BLEND_ADD);
      for (const Mote& m : motes_) {
        float x, y, a;
        motePose(m, now, x, y, a);
        if (a <= 0.004f) continue;
        moteBrush_->SetColor(D2D1::ColorF(0.85f, 0.97f, 0.91f, 0.38f * a));
        dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(x, y), 3.0f, 3.0f),
                        moteBrush_.Get());
        moteBrush_->SetColor(glassfx::white(0.72f * a));
        dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(x, y), 1.5f, 1.5f),
                        moteBrush_.Get());
      }
      dc->SetPrimitiveBlend(D2D1_PRIMITIVE_BLEND_SOURCE_OVER);
    }

    // 按压扩散环（原型 .pressring presshalo .6s：scale .36→1.75 过冲，opacity→0）
    if (ringGrad_ && !rings_.empty()) {
      for (auto it = rings_.begin(); it != rings_.end();) {
        const float p = (float)((now - it->t0) / 0.6);
        if (p >= 1.0f) {
          it = rings_.erase(it);
          continue;
        }
        const float e = 1.0f - (1.0f - p) * (1.0f - p) * (1.0f - p);
        const float radius = 65.0f * (0.36f + (1.75f - 0.36f) * e);
        const D2D1_RECT_F bounds = D2D1::RectF(it->x - radius, it->y - radius,
                                               it->x + radius, it->y + radius);
        dc->PushLayer(D2D1::LayerParameters1(bounds, nullptr,
                                             D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
                                             D2D1::Matrix3x2F::Identity(), 1.0f - p),
                      nullptr);
        ringGrad_->SetCenter(D2D1::Point2F(it->x, it->y));
        ringGrad_->SetRadiusX(radius);
        ringGrad_->SetRadiusY(radius);
        dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(it->x, it->y), radius, radius),
                        ringGrad_.Get());
        dc->PopLayer();
        ++it;
      }
    }

    // 弧线描边：hairline ink 13% 底 + ink 34% 微光（原型 glow .dock-arc .glow）；
    // connector=false（胶囊/罗盘）不画折线，上方环境光/粒子层照常
    if (g.connector && ctxBrushReady(dc)) {
      for (int pass = 0; pass < 2; ++pass) {
        moteBrush_->SetColor(glassfx::ink(pass == 0 ? 0.13f : 0.34f));
        for (size_t i = 0; i + 1 < g.items.size(); ++i) {
          const D2D1_POINT_2F a{ (float)g.items[i].x, (float)g.items[i].y };
          const D2D1_POINT_2F b{ (float)g.items[i + 1].x, (float)g.items[i + 1].y };
          dc->DrawLine(a, b, moteBrush_.Get(), 1.0f);
        }
      }
    }
  }

  // 球体底：悬停外发光 → 浮动阴影 → 中心光晕环 → 自适应毛玻璃（强模糊 + 低底色
  // + 顶部受光纵向渐变）→ 统一光源边缘亮边 → 顶部内高光/底部回落 → 1px 描边。
  // halfW>r 时项为胶囊：填充/裁剪/描边走圆角矩形，光晕/阴影/高光按椭圆横向外扩，
  // 边缘亮边用圆角矩形渐变描边近似（环带几何是圆专用）
  void drawOrbBack(ID2D1DeviceContext* dc, const OrbStyleCtx& ctx) const override {
    if (!dc || !ctx.d3d || !ctx.brush || ctx.r <= 0) return;
    ensure(dc, ctx.d3d);
    const D2D1_POINT_2F c = ctx.center;
    const float r = ctx.r;
    const float hw = ctx.halfW > 0 ? ctx.halfW : ctx.r;
    const float dim = ctx.dimmed;

    if (ctx.isHot && hotGlow_ && !glassfx::isPill(hw, r)) {  // 椭圆光晕仅圆项（原型 .orb.hot 专属；块状 hot 仅描边）
      hotGlow_->SetCenter(c);
      hotGlow_->SetRadiusX(glassfx::shapeRX(hw, r, 22.0f));
      hotGlow_->SetRadiusY(r + 22.0f);
      dc->FillEllipse(
          D2D1::Ellipse(c, glassfx::shapeRX(hw, r, 22.0f), r + 22.0f), hotGlow_.Get());
    }

    // 浮动阴影（原型 0 10px 30px black 30%）
    if (shadow_ && !glassfx::isPill(hw, r)) {  // 椭圆阴影仅圆项
      shadow_->SetCenter(D2D1::Point2F(c.x, c.y + 10.0f));
      shadow_->SetRadiusX(glassfx::shapeRX(hw, r, 15.0f));
      shadow_->SetRadiusY(r + 15.0f);
      dc->FillEllipse(D2D1::Ellipse(c, glassfx::shapeRX(hw, r, 15.0f), r + 15.0f),
                      shadow_.Get());
    }

    if (ctx.isCenter && !glassfx::isPill(hw, r)) {  // 中心光晕环仅圆项（原型 .orb.center 专属）
      ctx.brush->SetColor(glassfx::accentC(0.10f * dim));
      glassfx::drawShape(dc, c, hw, r, ctx.brush, 6.0f, 3.0f, ctx.cornerR);
    }

    // 自适应毛玻璃：形状域裁剪 → 模糊+提饱和+提亮(1.08)背景 → desk 12% 底 + 顶部受光
    bool glassDrawn = false;
    if (ctx.backdrop && ctx.backdrop->ok() && !ctx.backdrop->degraded()) {
      pipe_.refresh(dc, ctx.backdrop);
      lastLuma_ = ctx.backdrop->luma();
      if (pipe_.ready() && glassfx::pushShapeClip(dc, c, hw, r, ctx.cornerR)) {
        dc->DrawImage(pipe_.output(), D2D1::Point2F(ctx.backdropDX, ctx.backdropDY),
                      D2D1_INTERPOLATION_MODE_LINEAR);
        ctx.brush->SetColor(glassfx::desk(0.12f * dim));
        glassfx::fillShape(dc, c, hw, r, ctx.brush, ctx.cornerR);
        if (topLight_) {
          topLight_->SetStartPoint(D2D1::Point2F(c.x, c.y - r));
          topLight_->SetEndPoint(D2D1::Point2F(c.x, c.y + r));
          glassfx::fillShape(dc, c, hw, r, topLight_.Get(), ctx.cornerR);
        }
        dc->PopLayer();
        glassDrawn = true;
      }
    }
    if (!glassDrawn) {  // 退化：desk-deep 70% + desk 12% + 顶部受光
      ctx.brush->SetColor(glassfx::deskDeep(0.70f * dim));
      glassfx::fillShape(dc, c, hw, r, ctx.brush, ctx.cornerR);
      ctx.brush->SetColor(glassfx::desk(0.12f * dim));
      glassfx::fillShape(dc, c, hw, r, ctx.brush, ctx.cornerR);
      if (topLight_) {
        topLight_->SetStartPoint(D2D1::Point2F(c.x, c.y - r));
        topLight_->SetEndPoint(D2D1::Point2F(c.x, c.y + r));
        glassfx::fillShape(dc, c, hw, r, topLight_.Get(), ctx.cornerR);
      }
    }

    // 统一光源：朝向指针一侧边缘亮边（环带 55%~100% r，方向/距离衰减同 liquid；
    // 胶囊改圆角矩形 2.5px 渐变描边）
    const glassfx::PtrLight L = glassfx::ptrLight(px_, py_, hasPtr_, c);
    if (L.k > 0.01f && edgeLight_) {
      edgeLight_->SetCenter(
          D2D1::Point2F(c.x + L.ux * r * 0.75f, c.y + L.uy * r * 0.75f));
      edgeLight_->SetRadiusX(r * 0.95f + (glassfx::isPill(hw, r) ? hw - r : 0.0f));
      edgeLight_->SetRadiusY(r * 0.95f);
      const D2D1_RECT_F bounds = D2D1::RectF(c.x - hw - 1.0f, c.y - r - 1.0f,
                                             c.x + hw + 1.0f, c.y + r + 1.0f);
      if (!glassfx::isPill(hw, r)) {
        if (ComPtr<ID2D1Geometry> band = glassfx::ring(dc, c, r, r * 0.55f)) {
          dc->PushLayer(D2D1::LayerParameters1(bounds, band.Get(),
                                               D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
                                               D2D1::Matrix3x2F::Identity(),
                                               L.k * dim),
                        nullptr);
          const D2D1_ELLIPSE ball = D2D1::Ellipse(c, r, r);
          dc->FillEllipse(&ball, edgeLight_.Get());
          dc->PopLayer();
        }
      } else {
        dc->PushLayer(D2D1::LayerParameters1(bounds, nullptr,
                                             D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
                                             D2D1::Matrix3x2F::Identity(),
                                             L.k * dim),
                      nullptr);
        glassfx::drawShape(dc, c, hw, r, edgeLight_.Get(), 2.5f, -1.25f, ctx.cornerR);
        dc->PopLayer();
      }
    }

    // 顶部内高光 ink 28% + 底部回落 black 20%（原型 inset 组合）
    if (hlTop_) {
      hlTop_->SetCenter(D2D1::Point2F(c.x - 0.30f * r, c.y - 0.55f * r));
      hlTop_->SetRadiusX(1.2f * hw);
      hlTop_->SetRadiusY(1.2f * r);
      glassfx::fillShape(dc, c, hw, r, hlTop_.Get(), ctx.cornerR);
    }
    if (bottomShade_) {
      bottomShade_->SetCenter(D2D1::Point2F(c.x, c.y + 0.35f * r));
      bottomShade_->SetRadiusX(0.95f * hw);
      bottomShade_->SetRadiusY(0.95f * r);
      glassfx::fillShape(dc, c, hw, r, bottomShade_.Get(), ctx.cornerR);
    }

    // 1px 描边：悬停 accent > 中心 accent 60% > ink 15%（原型 glow border）
    if (ctx.isHot && !glassfx::isPill(hw, r))  // 块状项悬停不要 accent 描边（用户裁决：悬停=他项降暗）
      ctx.brush->SetColor(glassfx::accentC(1.0f * dim));
    else if (ctx.isCenter)
      ctx.brush->SetColor(glassfx::accentC(0.60f * dim));
    else
      ctx.brush->SetColor(glassfx::ink(0.15f * dim));
    glassfx::drawShape(dc, c, hw, r, ctx.brush, 1.0f, 0.0f, ctx.cornerR);
  }

  // 详情卡底：86% 深玻璃 + 白 8% 提亮 + ink 16% 描边（v1 卡不做 backdrop blur）
  void drawCardBack(ID2D1DeviceContext* dc, const D2D1_RECT_F& rect,
                    float radius) const override {
    if (!dc) return;
    ComPtr<ID2D1SolidColorBrush> brush;
    if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush))) return;
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(rect, radius, radius);
    brush->SetColor(glassfx::deskDeep(0.86f));
    dc->FillRoundedRectangle(&rr, brush.Get());
    brush->SetColor(glassfx::ink(0.08f));
    dc->FillRoundedRectangle(&rr, brush.Get());
    brush->SetColor(glassfx::ink(0.16f));
    dc->DrawRoundedRectangle(&rr, brush.Get(), 1.0f);
  }

private:
  bool ctxBrushReady(ID2D1DeviceContext* dc) const {
    if (moteBrush_) return true;
    return SUCCEEDED(
        dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &moteBrush_));
  }

  // 球底层画刷（设备无关：径向/线性渐变在 ensure 里按代际重建则浪费，
  // 这里随首个 dc 创建，设备重建后经 ensureOrb 的代际检查重建）
  void ensureBrushes(ID2D1DeviceContext* dc) const {
    if (brushesDc_ == dc && gasInk_) return;
    brushesDc_ = dc;
    gasInk_ = glassfx::radial(dc, {{0.0f, glassfx::ink(0.08f)},
                                   {0.72f, glassfx::ink(0.0f)},
                                   {1.0f, glassfx::ink(0.0f)}});
    gasAccent_ = glassfx::radial(dc, {{0.0f, glassfx::accentC(0.09f)},
                                      {0.70f, glassfx::accentC(0.0f)},
                                      {1.0f, glassfx::accentC(0.0f)}});
    litCore_ = glassfx::radial(dc, {{0.0f, glassfx::white(0.16f)},
                                    {0.70f, glassfx::white(0.0f)},
                                    {1.0f, glassfx::white(0.0f)}});
    litEdge_ = glassfx::radial(dc, {{0.0f, glassfx::accentC(0.13f)},
                                    {0.72f, glassfx::accentC(0.0f)},
                                    {1.0f, glassfx::accentC(0.0f)}});
    ringGrad_ = glassfx::radial(dc, {{0.0f, glassfx::white(0.36f)},
                                     {0.42f, glassfx::accentC(0.18f)},
                                     {0.68f, glassfx::accentC(0.0f)},
                                     {1.0f, glassfx::accentC(0.0f)}});
    if (gasInk_) {
      gasInk_->SetRadiusX(340.0f);
      gasInk_->SetRadiusY(480.0f);
      gasAccent_->SetRadiusX(300.0f);
      gasAccent_->SetRadiusY(430.0f);
      litCore_->SetRadiusX(120.0f);
      litCore_->SetRadiusY(120.0f);
      litEdge_->SetRadiusX(230.0f);
      litEdge_->SetRadiusY(230.0f);
    }
  }

  // 粒子推进：环境微粒首帧播种；汇聚按 260ms 节奏喷 3 粒；清理完结粒子
  void updateMotes(double now, float dt, const DockGeom& g, float side) const {
    (void)dt;
    if (!ambientSeeded_ && !g.items.empty()) {
      ambientSeeded_ = true;
      std::mt19937 rng(0x5FE0A8u);
      std::uniform_real_distribution<float> u(0.0f, 1.0f);
      for (int k = 0; k < 14; ++k) {
        Mote m;
        m.kind = 0;
        m.x = (float)g.w * (0.18f + 0.64f * u(rng));
        m.y = (float)g.h * (0.06f + 0.88f * u(rng));
        m.dx = u(rng) * 24.0f - 12.0f;
        m.dy = -76.0f;
        m.dur = 7.0 + 6.0 * u(rng);
        m.t0 = now - u(rng) * m.dur;
        m.maxA = 0.3f + 0.35f * u(rng);
        motes_.push_back(m);
      }
    }
    if (lit_ && now - lastGather_ > 0.26) {
      lastGather_ = now;
      std::mt19937 rng((unsigned)(now * 1000.0));
      std::uniform_real_distribution<float> u(0.0f, 1.0f);
      for (int k = 0; k < 3; ++k) {
        const float ang = u(rng) * 6.2832f;
        const float rad = 36.0f + 30.0f * u(rng);
        const float ox = (float)std::cos(ang) * rad;
        const float oy = (float)std::sin(ang) * rad;
        motes_.push_back({gxT_, gyT_, ox, oy, now, 0.62, 0.85f, 1});
      }
    }
    for (auto it = motes_.begin(); it != motes_.end();) {
      if (it->kind != 0 && now - it->t0 > it->dur)
        it = motes_.erase(it);
      else
        ++it;
    }
    if (motes_.size() > 96)  // 汇聚长驻上界
      motes_.erase(motes_.begin(), motes_.begin() + (ptrdiff_t)(motes_.size() - 96));
    side_ = side;
  }

  void burstAt(float x, float y) const {
    std::mt19937 rng((unsigned)(nowSeconds() * 1000.0) ^ 0x9e37);
    std::uniform_real_distribution<float> u(0.0f, 1.0f);
    const double now = nowSeconds();
    for (int k = 0; k < 5; ++k) {
      // 迸散偏向屏内侧（原型 inward = edge right ? -1 : 1）
      const float bx = -side_ * (14.0f + 34.0f * u(rng));
      const float by = u(rng) * 44.0f - 22.0f;
      motes_.push_back({x, y, bx, by, now, 0.72, 0.95f, 2});
    }
  }

  static void motePose(const Mote& m, double now, float& x, float& y, float& a) {
    const float p = (float)((now - m.t0) / m.dur);
    if (m.kind == 0) {  // 环境：循环漂浮，14%/82% 淡入淡出
      const float lp = p - std::floor(p);
      x = m.x + m.dx * lp;
      y = m.y + m.dy * lp;
      const float fadeIn = std::clamp(lp / 0.14f, 0.0f, 1.0f);
      const float fadeOut = lp > 0.82f ? (1.0f - lp) / 0.18f : 1.0f;
      a = m.maxA * fadeIn * fadeOut;
    } else if (m.kind == 1) {  // 汇聚：ease-in 收敛到目标，30% 处达峰后淡出
      const float e = (1.0f - p) * (1.0f - p);
      x = m.x + m.dx * e;
      y = m.y + m.dy * e;
      a = p < 0.3f ? m.maxA * (p / 0.3f) : m.maxA * (1.0f - p) / 0.7f;
    } else {  // 迸散：ease-out 外推 + 缩小
      const float e = 1.0f - (1.0f - p) * (1.0f - p) * (1.0f - p);
      x = m.x + m.dx * e;
      y = m.y + m.dy * e;
      a = m.maxA * (1.0f - p) * (1.0f - 0.7f * p);
    }
  }

  void ensure(ID2D1DeviceContext* dc, const D3DContext* d3d) const {
    if (dc == seenDc_ && gen_ == d3d->generation()) return;
    seenDc_ = dc;
    gen_ = d3d->generation();
    brushesDc_ = nullptr;  // 设备重建 → 球底层画刷下帧随新 dc 重建
    moteBrush_.Reset();
    pipe_.reset();
    hotGlow_.Reset();
    shadow_.Reset();
    topLight_.Reset();
    hlTop_.Reset();
    bottomShade_.Reset();
    edgeLight_.Reset();
    // σ13 ≈ CSS blur(26px)；CSS saturate(170%) → D2D 0.85；brightness(1.08)
    (void)pipe_.ensure(dc, 13.0f, 0.85f, 1.08f);
    hotGlow_ = glassfx::radial(dc, {{0.0f, glassfx::accentC(0.30f)},
                                    {0.50f, glassfx::accentC(0.15f)},
                                    {1.0f, glassfx::accentC(0.0f)}});
    shadow_ = glassfx::radial(dc, {{0.0f, D2D1::ColorF(0, 0, 0, 0.30f)},
                                   {0.60f, D2D1::ColorF(0, 0, 0, 0.12f)},
                                   {1.0f, D2D1::ColorF(0, 0, 0, 0.0f)}});
    topLight_ = glassfx::linear(dc, {{0.0f, glassfx::ink(0.11f)},
                                     {0.52f, glassfx::ink(0.03f)},
                                     {1.0f, glassfx::ink(0.0f)}});
    hlTop_ = glassfx::radial(dc, {{0.0f, glassfx::ink(0.28f)},
                                  {0.55f, glassfx::ink(0.0f)},
                                  {1.0f, glassfx::ink(0.0f)}});
    bottomShade_ = glassfx::radial(dc, {{0.0f, D2D1::ColorF(0, 0, 0, 0.0f)},
                                        {0.75f, D2D1::ColorF(0, 0, 0, 0.05f)},
                                        {1.0f, D2D1::ColorF(0, 0, 0, 0.20f)}});
    edgeLight_ = glassfx::radial(dc, {{0.0f, glassfx::white(0.30f)},
                                      {0.60f, glassfx::white(0.08f)},
                                      {1.0f, glassfx::white(0.0f)}});
  }

  mutable const ID2D1DeviceContext* seenDc_ = nullptr;
  mutable unsigned gen_ = 0;
  mutable glassfx::BackdropPipe pipe_;  // 强模糊 + 提饱和 + 提亮背景管线
  mutable float lastLuma_ = 0.0f;        // 最近背景帧亮度（自适应墨色）
  mutable ComPtr<ID2D1RadialGradientBrush> hotGlow_, shadow_, hlTop_, bottomShade_;
  mutable ComPtr<ID2D1RadialGradientBrush> edgeLight_;
  mutable ComPtr<ID2D1LinearGradientBrush> topLight_;
  // 球底层资源（drawArcStroke 时机；无 OrbStyleCtx.d3d 故按 dc 指针缓存）
  mutable const ID2D1DeviceContext* brushesDc_ = nullptr;
  mutable ComPtr<ID2D1RadialGradientBrush> gasInk_, gasAccent_, litCore_, litEdge_;
  mutable ComPtr<ID2D1RadialGradientBrush> ringGrad_;
  mutable ComPtr<ID2D1SolidColorBrush> moteBrush_;
  // 光源/粒子状态
  mutable float px_ = 0, py_ = 0;
  mutable bool hasPtr_ = false;
  mutable bool lit_ = false, litInit_ = false;
  mutable float litA_ = 0, gx_ = 0, gy_ = 0, gxT_ = 0, gyT_ = 0;
  mutable float side_ = 1.0f;  // 屏缘方向（+1 右缘），迸散偏向用
  mutable double lastT_ = 0, lastGather_ = 0;
  mutable bool ambientSeeded_ = false;
  mutable std::vector<Mote> motes_;
  mutable std::vector<PressRing> rings_;
};

} // namespace

std::unique_ptr<IMaterial> createGlowMaterial() {
  return std::unique_ptr<IMaterial>(new GlowMaterial());
}

} // namespace okmeter::render
