#include "settings.h"
#include "../render/catalog.h"
#include "../render/materials/glassfx.h"
#include <algorithm>
#include <cmath>

using Microsoft::WRL::ComPtr;

namespace okmeter {
namespace {

namespace gfx = render::glassfx;

// 原型 .panel/.dlg-sec 度量（okmeter-dock-prototype.html 设置面板同款）
constexpr float kSecPad = 14.0f;      // dlg-sec padding 14 0
constexpr float kH4H = 15.0f;         // 节标题行高
constexpr float kH4Gap = 10.0f;       // h4 margin-bottom
constexpr float kChoiceGap = 8.0f;    // choice-grid gap
constexpr float kFormTabH = 80.0f;    // 形态选项卡高（10+38+6+17+9）
constexpr float kMatTabH = 97.0f;     // 材质选项卡高（+3+14 副标）
constexpr float kChipH = 32.0f;       // chips 高（7×2+~18）
constexpr float kChipPadX = 14.0f;    // chips padding-x
constexpr float kMapRowH = 44.0f;     // map-row padding 5×2 + gsel 34
constexpr float kGselH = 34.0f;       // gsel padding 8×2 + ~18
constexpr float kPosColW = 132.0f;    // map-row 位置列宽
constexpr float kMapGap = 12.0f;      // map-row gap
constexpr float kDropItemH = 28.0f;   // drop button padding 7×2 + ~14
constexpr float kDropMaxH = 280.0f;   // drop max-height
constexpr float kSwitchW = 40.0f, kSwitchH = 22.0f;  // .sw
constexpr float kContentW = kSettingsPanelW - 2 * 15.0f;  // 298

// 形态/材质选项卡内容全部来自渲染模块目录（render/catalog.h kFormCatalog/
// kMaterialCatalog：id/显示名/副标题/缩略图种类），新增模块无需改本文件
constexpr int kCounts[] = {1, 3, 5, 7};

std::wstring wide(const std::string& s) {
  if (s.empty()) return {};
  const int n = MultiByteToWideChar(CP_UTF8, 0, s.c_str(), (int)s.size(), nullptr, 0);
  std::wstring out((size_t)n, L'\0');
  MultiByteToWideChar(CP_UTF8, 0, s.c_str(), (int)s.size(), out.data(), n);
  return out;
}

// 路径记录器：SVG 子集（M/L/Q/C/A/Z）→ ID2D1PathGeometry（缩略图矢量用）。
// Q 转三次贝塞尔；A 取 SVG 椭圆弧（仅正交轴：large/sweep 两标志）。
class PathInk {
public:
  bool open(ID2D1DeviceContext* dc) {
    ComPtr<ID2D1Factory> f;
    dc->GetFactory(&f);
    if (!f || FAILED(f->CreatePathGeometry(&geo_)) || FAILED(geo_->Open(&sink_)))
      return false;
    return true;
  }
  void m(float x, float y) {
    if (fig_) sink_->EndFigure(D2D1_FIGURE_END_OPEN);
    sink_->BeginFigure(D2D1::Point2F(x, y), D2D1_FIGURE_BEGIN_HOLLOW);
    fig_ = true;
    cx_ = x; cy_ = y;
  }
  void l(float x, float y) { sink_->AddLine(D2D1::Point2F(x, y)); cx_ = x; cy_ = y; }
  void q(float qx, float qy, float x, float y) {  // 二次 → 三次
    c(cx_ + (qx - cx_) * (2.0f / 3.0f), cy_ + (qy - cy_) * (2.0f / 3.0f),
      x + (qx - x) * (2.0f / 3.0f), y + (qy - y) * (2.0f / 3.0f), x, y);
  }
  void c(float x1, float y1, float x2, float y2, float x, float y) {
    sink_->AddBezier(D2D1::BezierSegment(D2D1::Point2F(x1, y1),
                                         D2D1::Point2F(x2, y2),
                                         D2D1::Point2F(x, y)));
    cx_ = x; cy_ = y;
  }
  void a(float rx, float ry, bool sweep, float x, float y) {
    D2D1_ARC_SEGMENT seg{};
    seg.point = D2D1::Point2F(x, y);
    seg.size = D2D1::SizeF(rx, ry);
    seg.sweepDirection = sweep ? D2D1_SWEEP_DIRECTION_CLOCKWISE
                               : D2D1_SWEEP_DIRECTION_COUNTER_CLOCKWISE;
    seg.arcSize = D2D1_ARC_SIZE_SMALL;
    sink_->AddArc(&seg);
    cx_ = x; cy_ = y;
  }
  void z() { sink_->EndFigure(D2D1_FIGURE_END_CLOSED); fig_ = false; }
  ID2D1PathGeometry* done() {
    if (fig_) sink_->EndFigure(D2D1_FIGURE_END_OPEN);
    fig_ = false;
    (void)sink_->Close();
    return geo_.Get();
  }

private:
  ComPtr<ID2D1PathGeometry> geo_;
  ComPtr<ID2D1GeometrySink> sink_;
  bool fig_ = false;
  float cx_ = 0, cy_ = 0;
};

} // namespace

bool SettingsPanel::ensure(render::D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  if (!dc || !d3d.dwrite()) return false;
  if (dc == seen_ && seenGen_ == d3d.generation() && brush_ && dashStyle_ &&
      titleFmt_ && enFmt_ && h4Fmt_ && nameFmt_ && smallFmt_ && bodyFmt_ &&
      posFmt_ && btnFmt_)
    return true;
  seen_ = dc;
  seenGen_ = d3d.generation();
  brush_.Reset();
  dashStyle_.Reset();
  titleFmt_.Reset(); enFmt_.Reset(); h4Fmt_.Reset(); nameFmt_.Reset();
  smallFmt_.Reset(); bodyFmt_.Reset(); posFmt_.Reset(); btnFmt_.Reset();
  if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush_))) return false;
  ComPtr<ID2D1Factory> f;
  dc->GetFactory(&f);
  if (f) {
    const float dashes[] = {2.0f, 3.0f};  // 原型 stroke-dasharray="2 3"
    D2D1_STROKE_STYLE_PROPERTIES sp{};
    sp.dashStyle = D2D1_DASH_STYLE_CUSTOM;
    (void)f->CreateStrokeStyle(&sp, dashes, 2, &dashStyle_);
  }
  if (!dashStyle_) return false;
  IDWriteFactory* dw = d3d.dwrite();
  auto make = [&](float size, DWRITE_FONT_WEIGHT weight,
                  DWRITE_TEXT_ALIGNMENT halign, IDWriteTextFormat** out) {
    if (FAILED(dw->CreateTextFormat(L"Segoe UI", nullptr, weight,
                                    DWRITE_FONT_STYLE_NORMAL, DWRITE_FONT_STRETCH_NORMAL,
                                    size, L"", out)))
      return false;
    (*out)->SetTextAlignment(halign);
    (*out)->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
    return true;
  };
  return make(13.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD, DWRITE_TEXT_ALIGNMENT_LEADING,
              &titleFmt_) &&
         make(9.0f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_TRAILING,
              &enFmt_) &&
         make(10.5f, DWRITE_FONT_WEIGHT_SEMI_BOLD, DWRITE_TEXT_ALIGNMENT_LEADING,
              &h4Fmt_) &&
         make(12.0f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_CENTER,
              &nameFmt_) &&
         make(10.5f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_CENTER,
              &smallFmt_) &&
         make(12.5f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_LEADING,
              &bodyFmt_) &&
         make(12.0f, DWRITE_FONT_WEIGHT_MEDIUM, DWRITE_TEXT_ALIGNMENT_CENTER,
              &btnFmt_) &&
         [&] {  // 位置标签 Consolas 11.5（原型 .map-row .pos font-mono）
           if (FAILED(dw->CreateTextFormat(L"Consolas", nullptr,
                                           DWRITE_FONT_WEIGHT_NORMAL,
                                           DWRITE_FONT_STYLE_NORMAL,
                                           DWRITE_FONT_STRETCH_NORMAL,
                                           11.5f, L"", &posFmt_)))
             return false;
           posFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
           posFmt_->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
           return true;
         }();
}

void SettingsPanel::begin(const Config& cur, std::vector<std::string> models) {
  draft = cur;
  models_ = std::move(models);
  open = true;
  scrollY = 0;
  hover = -1;
  dropGsel_ = -1;
  dropScroll_ = 0;
}

std::vector<std::pair<std::string, std::wstring>>
SettingsPanel::optionsFor(int slot) const {
  (void)slot;  // 各槽位选项全集相同
  std::vector<std::pair<std::string, std::wstring>> opts;
  opts.emplace_back("auto", L"默认 · 按最近使用");
  opts.emplace_back("total:session", L"总量 · 当前会话");
  opts.emplace_back("total:today", L"总量 · 今日");
  opts.emplace_back("total:week", L"总量 · 本周");
  opts.emplace_back("total:all", L"总量 · 全部累计");
  for (const std::string& id : models_)
    opts.emplace_back("model:" + id, L"模型 · " + wide(id));
  return opts;
}

std::wstring SettingsPanel::valueLabel(const std::string& v) const {
  if (v == "auto" || v.empty()) return L"默认 · 按最近使用";
  if (v == "total:session") return L"总量 · 当前会话";
  if (v == "total:today") return L"总量 · 今日";
  if (v == "total:week") return L"总量 · 本周";
  if (v == "total:all") return L"总量 · 全部累计";
  if (v.rfind("model:", 0) == 0) return L"模型 · " + wide(v.substr(6));
  return L"默认 · 按最近使用";
}

void SettingsPanel::layout(render::D3DContext& d3d) {
  ctrls_.clear();
  secs_.clear();
  posLabels_.clear();
  hover = -1;
  dropGsel_ = -1;
  dropScroll_ = 0;
  // 头/尾三按钮固定前三个（矩形由 place 填充）
  ctrls_.push_back(Ctrl{Ctrl::CloseX, 0, 0, {}, false});
  ctrls_.push_back(Ctrl{Ctrl::CancelBtn, 0, 0, {}, false});
  ctrls_.push_back(Ctrl{Ctrl::SaveBtn, 0, 0, {}, false});

  const bool hasDev = ensure(d3d);
  IDWriteFactory* dw = hasDev ? d3d.dwrite() : nullptr;
  auto mwidth = [&](IDWriteTextFormat* fmt, const std::wstring& s) {
    if (dw && fmt) {
      ComPtr<IDWriteTextLayout> tl;
      if (SUCCEEDED(dw->CreateTextLayout(s.c_str(), (UINT32)s.size(), fmt,
                                         2000.0f, 100.0f, &tl))) {
        DWRITE_TEXT_METRICS m{};
        if (SUCCEEDED(tl->GetMetrics(&m))) return m.width;
      }
    }
    float w = 0;
    for (wchar_t ch : s) w += ch < 128 ? 7.0f : 13.0f;
    return w;
  };

  float y = 12.0f;  // p-bd padding-top 12
  auto secBegin = [&](const wchar_t* title) {
    Sec s;
    s.title = title ? title : L"";
    s.y0 = y;
    s.titleW = s.title.empty() ? 0.0f : mwidth(h4Fmt_.Get(), s.title);
    secs_.push_back(s);
    y += kSecPad + (s.title.empty() ? 0.0f : kH4H + kH4Gap);
  };
  auto secEnd = [&] {
    y += kSecPad;
    secs_.back().y1 = y;
  };

  // ── 视觉形态：目录项 2 列选项卡（原型 choice-grid）──
  secBegin(L"视觉形态");
  {
    const float iw = (kContentW - kChoiceGap) * 0.5f;
    const int rows = (render::kFormCount + 1) / 2;
    for (int i = 0; i < render::kFormCount; ++i) {
      const float cx = (float)(i % 2) * (iw + kChoiceGap);
      const float cy = y + (float)(i / 2) * (kFormTabH + kChoiceGap);
      Ctrl c;
      c.kind = Ctrl::FormTab;
      c.a = i;
      c.rc = D2D1::RectF(cx, cy, cx + iw, cy + kFormTabH);
      ctrls_.push_back(c);
    }
    y += (float)rows * kFormTabH + (float)(rows - 1) * kChoiceGap;
  }
  secEnd();

  // ── 材质效果：目录项 2 列选项卡（带副标）──
  secBegin(L"材质效果");
  {
    const float iw = (kContentW - kChoiceGap) * 0.5f;
    const int rows = (render::kMaterialCount + 1) / 2;
    for (int i = 0; i < render::kMaterialCount; ++i) {
      const float cx = (float)(i % 2) * (iw + kChoiceGap);
      const float cy = y + (float)(i / 2) * (kMatTabH + kChoiceGap);
      Ctrl c;
      c.kind = Ctrl::MaterialTab;
      c.a = i;
      c.rc = D2D1::RectF(cx, cy, cx + iw, cy + kMatTabH);
      ctrls_.push_back(c);
    }
    y += (float)rows * kMatTabH + (float)(rows - 1) * kChoiceGap;
  }
  secEnd();

  // ── 球数量 chips 1/3/5/7 ──
  secBegin(L"球数量（仅奇数）");
  {
    float cx = 0;
    for (int i = 0; i < 4; ++i) {
      const std::wstring t = std::to_wstring(kCounts[i]);
      const float cw = mwidth(bodyFmt_.Get(), t) + 2.0f * kChipPadX;
      Ctrl c;
      c.kind = Ctrl::CountChip;
      c.a = i;
      c.rc = D2D1::RectF(cx, y, cx + cw, y + kChipH);
      ctrls_.push_back(c);
      cx += cw + kChoiceGap;
    }
    y += kChipH;
  }
  secEnd();

  // ── 指标映射 · 按位置：每槽位一行（pos + 自绘玻璃下拉）──
  secBegin(L"指标映射 · 按位置");
  for (int slot = 0; slot < draft.count; ++slot) {
    const int mid = (draft.count - 1) / 2;
    const int d = slot - mid;
    std::wstring pos = L"位置 " + std::to_wstring(slot + 1) + L" · " +
        (d == 0 ? L"居中" : (d < 0 ? L"上 " + std::to_wstring(-d)
                                   : L"下 " + std::to_wstring(d)));
    posLabels_.emplace_back(std::move(pos),
        D2D1::RectF(0.0f, y + 5.0f, kPosColW, y + 5.0f + kGselH));
    Ctrl c;
    c.kind = Ctrl::Gsel;
    c.a = slot;
    const float gx = kPosColW + kMapGap;
    c.rc = D2D1::RectF(gx, y + 5.0f, kContentW, y + 5.0f + kGselH);
    ctrls_.push_back(c);
    y += kMapRowH;
  }
  secEnd();

  // ── 吸附边 chips 左/右 ──
  secBegin(L"吸附边");
  {
    float cx = 0;
    const wchar_t* names[] = {L"左", L"右"};
    for (int i = 0; i < 2; ++i) {
      const float cw = mwidth(bodyFmt_.Get(), names[i]) + 2.0f * kChipPadX;
      Ctrl c;
      c.kind = Ctrl::EdgeChip;
      c.a = i;
      c.rc = D2D1::RectF(cx, y, cx + cw, y + kChipH);
      ctrls_.push_back(c);
      cx += cw + kChoiceGap;
    }
    y += kChipH;
  }
  secEnd();

  // ── mergeCache 玻璃开关（switch-row，无节标题）──
  secBegin(nullptr);
  swText_ = D2D1::RectF(0.0f, y, kContentW - kSwitchW - 16.0f, y + 18.0f);
  swSmall_ = D2D1::RectF(0.0f, y + 20.0f, kContentW - kSwitchW - 16.0f, y + 36.0f);
  {
    Ctrl c;
    c.kind = Ctrl::MergeSwitch;
    c.rc = D2D1::RectF(kContentW - kSwitchW, y + 6.0f, kContentW, y + 6.0f + kSwitchH);
    ctrls_.push_back(c);
  }
  y += 36.0f;
  secEnd();

  contentH_ = (int)std::ceil(y + 16.0f - 12.0f);  // p-bd padding-bottom 16 换底 padding
  saveBtnW_ = mwidth(btnFmt_.Get(), L"保存并生效") + 28.0f;  // btn padding 8 14
  cancelBtnW_ = mwidth(btnFmt_.Get(), L"取消") + 28.0f;
  scrollY = (std::min)(scrollY, maxScroll());
}

void SettingsPanel::place(float x, float y, float h) {
  rect = D2D1::RectF(x, y, x + (float)kSettingsPanelW, y + h);
  const float w = (float)kSettingsPanelW;
  ctrls_[0].rc = D2D1::RectF(w - 15.0f - 22.0f, 12.0f, w - 15.0f, 34.0f);  // ✕
  const float ftY = h - kFtH;
  const float by0 = ftY + (kFtH - 33.0f) * 0.5f;
  const float sx1 = w - 15.0f;
  ctrls_[2].rc = D2D1::RectF(sx1 - saveBtnW_, by0, sx1, by0 + 33.0f);       // 保存并生效
  ctrls_[1].rc = D2D1::RectF(sx1 - saveBtnW_ - 8.0f - cancelBtnW_, by0,
                             sx1 - saveBtnW_ - 8.0f, by0 + 33.0f);          // 取消
  scrollY = (std::min)(scrollY, maxScroll());
}

int SettingsPanel::maxScroll() const {
  return (std::max)(0, contentH_ - (int)bodyH());
}

void SettingsPanel::closeDrop() {
  if (dropGsel_ < 0) return;
  ctrls_.resize(dropStart_);
  dropGsel_ = -1;
  dropScroll_ = 0;
}

void SettingsPanel::openDrop(render::D3DContext& d3d, int gselCtrl) {
  closeDrop();
  const Ctrl& g = ctrls_[(size_t)gselCtrl];
  const auto opts = optionsFor(g.a);
  const bool hasDev = ensure(d3d);
  float tw = g.rc.right - g.rc.left;  // min-width = 按钮宽（原型同款）
  if (hasDev) {
    for (const auto& o : opts) {
      ComPtr<IDWriteTextLayout> tl;
      if (FAILED(d3d.dwrite()->CreateTextLayout(o.second.c_str(),
                                                (UINT32)o.second.size(),
                                                bodyFmt_.Get(), 2000.0f, 100.0f, &tl)))
        continue;
      DWRITE_TEXT_METRICS m{};
      if (FAILED(tl->GetMetrics(&m))) continue;
      const float need = m.width + 20.0f + 26.0f;  // padding 10×2 + ✓ 列
      if (need > tw) tw = need;
    }
  }
  const float panelW = rect.right - rect.left;
  const float panelH = rect.bottom - rect.top;
  if (tw > panelW - 16.0f) tw = panelW - 16.0f;
  float dx = kPadX + g.rc.left;
  if (dx + tw > panelW - 8.0f) dx = panelW - 8.0f - tw;
  const int n = (int)opts.size();
  const int vis = (std::min)(n, (int)((kDropMaxH - 10.0f) / kDropItemH));
  const float dh = 10.0f + (float)vis * kDropItemH;
  const float gy = kHdH + g.rc.top - (float)scrollY;  // gsel 面板坐标 y
  float dy = gy + (g.rc.bottom - g.rc.top) + 6.0f;    // 默认下展
  if (dy + dh > panelH - 8.0f) dy = gy - 6.0f - dh;   // 放不下改上展
  if (dy < 8.0f) dy = 8.0f;
  dropRc_ = D2D1::RectF(dx, dy, dx + tw, dy + dh);
  dropStart_ = ctrls_.size();
  for (int i = 0; i < n; ++i) {
    Ctrl c;
    c.kind = Ctrl::DropItem;
    c.a = g.a;
    c.b = i;
    c.body = false;
    c.rc = D2D1::RectF(dx + 5.0f, dy + 5.0f + (float)i * kDropItemH,
                       dx + tw - 5.0f, dy + 5.0f + (float)(i + 1) * kDropItemH);
    ctrls_.push_back(c);
  }
  dropGsel_ = gselCtrl;
  dropScroll_ = 0;
}

int SettingsPanel::click(render::D3DContext& d3d, int idx) {
  if (idx < 0 || idx >= (int)ctrls_.size()) return 0;
  const Ctrl& c = ctrls_[(size_t)idx];
  switch (c.kind) {
  case Ctrl::FormTab:
    draft.form = render::kFormCatalog[c.a].id;
    return 1;
  case Ctrl::MaterialTab:
    draft.material = render::kMaterialCatalog[c.a].id;
    return 1;
  case Ctrl::CountChip:
    draft.count = kCounts[c.a];
    draft.mapping.resize((size_t)draft.count, "auto");
    for (auto& m : draft.mapping) if (m.empty()) m = "auto";
    return 2;  // 映射行数变化 → 重排
  case Ctrl::EdgeChip:
    draft.edge = c.a == 0 ? "left" : "right";
    return 1;
  case Ctrl::MergeSwitch:
    draft.mergeCache = !draft.mergeCache;
    return 1;
  case Ctrl::Gsel:
    if (dropGsel_ == idx) closeDrop();
    else openDrop(d3d, idx);
    return 1;
  case Ctrl::DropItem: {
    const auto opts = optionsFor(c.a);
    if (c.b >= 0 && c.b < (int)opts.size())
      draft.mapping[(size_t)c.a] = opts[(size_t)c.b].first;
    closeDrop();
    return 1;
  }
  default:
    return 0;  // CloseX/CancelBtn/SaveBtn 由 app 处理
  }
}

int SettingsPanel::hit(int x, int y) const {
  if (!open) return -1;
  const float px = (float)x - rect.left;
  const float py = (float)y - rect.top;
  // 下拉浮层（面板坐标，仅可见行内命中；行随内部滚动换算）
  if (dropGsel_ >= 0 && px >= dropRc_.left && px < dropRc_.right &&
      py >= dropRc_.top && py < dropRc_.bottom) {
    const int row = (int)((py - dropRc_.top - 5.0f + (float)dropScroll_) / kDropItemH);
    for (size_t i = dropStart_; i < ctrls_.size(); ++i)
      if (ctrls_[i].b == row) return (int)i;
    return -1;  // 浮层 padding 区
  }
  const float panelW = rect.right - rect.left;
  const float panelH = rect.bottom - rect.top;
  if (px < 0 || px >= panelW || py < 0 || py >= panelH) return -1;
  // 头/尾按钮（前三个固定）
  for (int i = 0; i < 3; ++i) {
    const D2D1_RECT_F& r = ctrls_[(size_t)i].rc;
    if (px >= r.left && px < r.right && py >= r.top && py < r.bottom) return i;
  }
  // 体区控件（内容坐标换算）
  if (py >= kHdH && py < panelH - kFtH) {
    const float cx = px - kPadX;
    const float cy = py - kHdH + (float)scrollY;
    if (cx >= 0 && cx < kContentW) {
      const size_t end = dropGsel_ >= 0 ? dropStart_ : ctrls_.size();
      for (size_t i = 3; i < end; ++i) {
        const Ctrl& c = ctrls_[i];
        if (!c.body) continue;
        if (cx >= c.rc.left && cx < c.rc.right && cy >= c.rc.top && cy < c.rc.bottom)
          return (int)i;
      }
    }
  }
  return -1;
}

bool SettingsPanel::contains(int x, int y) const {
  if (!open) return false;
  const float px = (float)x - rect.left;
  const float py = (float)y - rect.top;
  if (dropGsel_ >= 0 && px >= dropRc_.left && px < dropRc_.right &&
      py >= dropRc_.top && py < dropRc_.bottom)
    return true;
  return px >= 0 && px < rect.right - rect.left && py >= 0 &&
         py < rect.bottom - rect.top;
}

int SettingsPanel::gselCtrl(int slot) const {
  for (size_t i = 3; i < ctrls_.size(); ++i)
    if (ctrls_[i].kind == Ctrl::Gsel && ctrls_[i].a == slot) return (int)i;
  return -1;
}

bool SettingsPanel::wheelAt(int x, int y, int delta) {
  if (!open) return false;
  const float px = (float)x - rect.left;
  const float py = (float)y - rect.top;
  const int step = (delta / 120) * 44;  // 上滚 delta>0 → scrollY 减
  if (dropGsel_ >= 0 && px >= dropRc_.left && px < dropRc_.right &&
      py >= dropRc_.top && py < dropRc_.bottom) {
    const int n = (int)(ctrls_.size() - dropStart_);
    const int maxS =
        (std::max)(0, (int)(10.0f + (float)n * kDropItemH -
                            (dropRc_.bottom - dropRc_.top)));
    const int ns = (std::min)((std::max)(0, dropScroll_ - step), maxS);
    if (ns == dropScroll_) return false;
    dropScroll_ = ns;
    return true;
  }
  const float panelH = rect.bottom - rect.top;
  if (px < 0 || px >= rect.right - rect.left || py < kHdH || py >= panelH - kFtH)
    return false;
  closeDrop();  // 体区滚动即关浮层（原型 .p-bd scroll → closeDrop 同款）
  const int ns = (std::min)((std::max)(0, scrollY - step), maxScroll());
  if (ns == scrollY) return false;
  scrollY = ns;
  hover = -1;
  return true;
}

void SettingsPanel::drawTextTrimmed(render::D3DContext& d3d, const std::wstring& s,
                                    IDWriteTextFormat* fmt, const D2D1_RECT_F& rc,
                                    D2D1_COLOR_F color) const {
  ComPtr<IDWriteTextLayout> tl;
  if (FAILED(d3d.dwrite()->CreateTextLayout(s.c_str(), (UINT32)s.size(), fmt,
                                            rc.right - rc.left, rc.bottom - rc.top, &tl)))
    return;
  DWRITE_TRIMMING trim{DWRITE_TRIMMING_GRANULARITY_CHARACTER, 0, 0};
  (void)tl->SetTrimming(&trim, nullptr);
  (void)tl->SetWordWrapping(DWRITE_WORD_WRAPPING_NO_WRAP);
  brush_->SetColor(color);
  d3d.dc()->DrawTextLayout(D2D1::Point2F(rc.left, rc.top), tl.Get(), brush_.Get());
}

void SettingsPanel::drawThumb(ID2D1DeviceContext* dc, int kind, int idx,
                              const D2D1_RECT_F& box, D2D1_COLOR_F color) const {
  const float bw = box.right - box.left;
  const float bh = box.bottom - box.top;
  const float s = (std::min)(bw / 100.0f, bh / 44.0f);
  const float ox = box.left + (bw - 100.0f * s) * 0.5f;
  const float oy = box.top + (bh - 44.0f * s) * 0.5f;
  D2D1_MATRIX_3X2_F old;
  dc->GetTransform(&old);
  dc->SetTransform(D2D1::Matrix3x2F::Scale(s, s) *
                   D2D1::Matrix3x2F::Translation(ox, oy) * old);
  brush_->SetColor(color);
  const float sw = 1.4f;  // 原型 svg stroke-width 1.4（随缩放等比）
  auto stroke = [&](ID2D1Geometry* g) {
    dc->DrawGeometry(g, brush_.Get(), sw);
  };
  auto line = [&](float x0, float y0, float x1, float y1) {
    dc->DrawLine(D2D1::Point2F(x0, y0), D2D1::Point2F(x1, y1), brush_.Get(), sw);
  };
  auto dashLine = [&](float x0, float y0, float x1, float y1) {
    dc->DrawLine(D2D1::Point2F(x0, y0), D2D1::Point2F(x1, y1), brush_.Get(), sw,
                 dashStyle_.Get());
  };
  auto circle = [&](float cx, float cy, float r) {
    dc->DrawEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), r, r), brush_.Get(), sw);
  };
  auto dot = [&](float cx, float cy, float r) {
    dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), r, r), brush_.Get());
  };
  auto rrect = [&](float x, float y, float w, float h, float r) {
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(D2D1::RectF(x, y, x + w, y + h), r, r);
    dc->DrawRoundedRectangle(&rr, brush_.Get(), sw);
  };

  if (kind == 0) {  // ── 形态（原型 form-choices svg）──
    if (idx == 0) {          // arc：M14 40 Q50 -14 86 40 + 三圆
      PathInk p;
      if (p.open(dc)) { p.m(14, 40); p.q(50, -14, 86, 40); stroke(p.done()); }
      circle(50, 12, 5); circle(24, 30, 4); circle(76, 30, 4);
    } else if (idx == 1) {   // capsule：三个圆角条
      rrect(14, 4, 72, 9, 4.5f);
      rrect(22, 18, 56, 9, 4.5f);
      rrect(30, 32, 40, 9, 4.5f);
    } else {                 // compass：虚线轨道 + 中心 + 四卫星
      dc->DrawEllipse(D2D1::Ellipse(D2D1::Point2F(50, 22), 17, 17), brush_.Get(),
                      sw, dashStyle_.Get());
      circle(50, 22, 6);
      circle(50, 5, 3); circle(67, 22, 3); circle(50, 39, 3); circle(33, 22, 3);
    }
  } else {                    // ── 材质（原型 material-choices svg）──
    if (idx == 0) {          // dark：圆 + 实心点 + 四刻度
      circle(50, 22, 14);
      dot(50, 22, 5);
      line(50, 3, 50, 7); line(50, 37, 50, 41);
      line(22, 22, 26, 22); line(74, 22, 78, 22);
    } else if (idx == 1) {   // frost：圆角矩形 + 三虚线
      rrect(26, 6, 48, 32, 9);
      dashLine(33, 15, 67, 15); dashLine(33, 22, 67, 22); dashLine(33, 29, 57, 29);
    } else if (idx == 2) {   // liquid：水滴 + 内弧
      PathInk p;
      if (p.open(dc)) {
        p.m(50, 4);
        p.c(58, 14, 64, 20, 64, 27);
        p.a(14, 14, true, 36, 27);
        p.c(36, 20, 42, 14, 50, 4);
        p.z();
        p.m(44, 27);
        p.a(7, 7, false, 50, 34);
        stroke(p.done());
      }
    } else {                 // glow：圆 + 八向光线
      circle(50, 22, 9);
      line(50, 4, 50, 9); line(50, 35, 50, 40);
      line(24, 22, 29, 22); line(71, 22, 76, 22);
      line(31.5f, 8.5f, 35, 12); line(68.5f, 8.5f, 65, 12);
      line(31.5f, 35.5f, 35, 32); line(68.5f, 35.5f, 65, 32);
    }
  }
  dc->SetTransform(old);
}

void SettingsPanel::draw(render::D3DContext& d3d, render::IMaterial& material) {
  if (!open || !ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);
  D2D1_MATRIX_3X2_F baseTm;
  dc->GetTransform(&baseTm);  // 可能带 app 的滑入动画平移，内部变换一律叠乘

  material.drawCardBack(dc, rect, 14.0f);  // 面板底跟随当前生效材质（圆角 14）

  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  auto hairline = [&](float x0, float y0, float x1, float y1) {
    brush_->SetColor(gfx::ink(0.13f));
    dc->DrawLine(D2D1::Point2F(x0, y0), D2D1::Point2F(x1, y1), brush_.Get(), 1.0f);
  };

  // ── 标题栏（p-hd）：accent 圆点 + "OkMeter 设置" + SETTINGS + ✕ ──
  {
    const float cx = rect.left + 15.0f + 3.5f;
    const float cy = rect.top + kHdH * 0.5f;
    brush_->SetColor(gfx::accentC(0.30f));
    dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), 5.5f, 5.5f), brush_.Get());
    brush_->SetColor(gfx::accentC(1.0f));
    dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), 3.5f, 3.5f), brush_.Get());
    txt(L"OkMeter 设置", titleFmt_.Get(),
        D2D1::RectF(rect.left + 31.0f, rect.top, rect.left + 180.0f, rect.top + kHdH),
        gfx::ink(0.95f));
    txt(L"SETTINGS", enFmt_.Get(),
        D2D1::RectF(rect.left + 150.0f, rect.top, ctrls_[0].rc.left + rect.left - 9.0f,
                    rect.top + kHdH),
        gfx::ink(0.45f));
    // ✕ 按钮（悬停 ink 11% 底）
    const D2D1_RECT_F xr = D2D1::RectF(ctrls_[0].rc.left + rect.left,
                                       ctrls_[0].rc.top + rect.top,
                                       ctrls_[0].rc.right + rect.left,
                                       ctrls_[0].rc.bottom + rect.top);
    brush_->SetColor(gfx::ink(hover == 0 ? 0.11f : 0.05f));
    const D2D1_ROUNDED_RECT xrr = D2D1::RoundedRect(xr, 6.0f, 6.0f);
    dc->FillRoundedRectangle(&xrr, brush_.Get());
    brush_->SetColor(gfx::ink(0.13f));
    dc->DrawRoundedRectangle(&xrr, brush_.Get(), 1.0f);
    brush_->SetColor(gfx::ink(hover == 0 ? 0.90f : 0.55f));
    const float gx = (xr.left + xr.right) * 0.5f;
    const float gy = (xr.top + xr.bottom) * 0.5f;
    dc->DrawLine(D2D1::Point2F(gx - 3.5f, gy - 3.5f), D2D1::Point2F(gx + 3.5f, gy + 3.5f),
                 brush_.Get(), 1.3f);
    dc->DrawLine(D2D1::Point2F(gx - 3.5f, gy + 3.5f), D2D1::Point2F(gx + 3.5f, gy - 3.5f),
                 brush_.Get(), 1.3f);
    hairline(rect.left, rect.top + kHdH - 0.5f, rect.right, rect.top + kHdH - 0.5f);
  }

  // ── 体区（p-bd）：裁剪 + 滚动平移，内容坐标绘制 ──
  const D2D1_RECT_F bodyRc =
      D2D1::RectF(rect.left, rect.top + kHdH, rect.right, rect.bottom - kFtH);
  dc->PushAxisAlignedClip(&bodyRc, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTransform(D2D1::Matrix3x2F::Translation(rect.left + kPadX,
                                                 rect.top + kHdH - (float)scrollY) *
                   baseTm);
  {
    // 节分隔线 + 节标题（标题后接渐隐 hairline，原型 h4::after 同款，取实线近似）
    for (const Sec& s : secs_) {
      if (s.y0 > 12.0f)  // 首节无边线
        hairline(0.0f, s.y0 + 0.5f, kContentW, s.y0 + 0.5f);
      if (s.title.empty()) continue;
      const float ty = s.y0 + kSecPad;
      txt(s.title, h4Fmt_.Get(), D2D1::RectF(0.0f, ty, kContentW, ty + kH4H),
          gfx::ink(0.50f));
      brush_->SetColor(gfx::ink(0.10f));
      dc->DrawLine(D2D1::Point2F(s.titleW + 8.0f, ty + kH4H * 0.5f),
                   D2D1::Point2F(kContentW, ty + kH4H * 0.5f), brush_.Get(), 1.0f);
    }
    // 映射行位置标签（Consolas 11.5 ink-dim）
    for (const auto& pl : posLabels_)
      txt(pl.first, posFmt_.Get(), pl.second, gfx::ink(0.50f));
    // 开关行文案
    txt(L"数字合并 cache 命中进 input", bodyFmt_.Get(), swText_, gfx::ink(0.90f));
    smallFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    txt(L"关闭后，详情卡将 input 拆为 常规 / cache 读 / cache 新建 三行",
        smallFmt_.Get(), swSmall_, gfx::ink(0.50f));
    smallFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);

    // 控件（体区内容坐标）
    const size_t end = dropGsel_ >= 0 ? dropStart_ : ctrls_.size();
    for (size_t i = 3; i < end; ++i) {
      const Ctrl& c = ctrls_[i];
      if (!c.body) continue;
      const bool hov = (int)i == hover;
      switch (c.kind) {
      case Ctrl::FormTab:
      case Ctrl::MaterialTab: {
        const render::ModuleMeta& meta = c.kind == Ctrl::FormTab
            ? render::kFormCatalog[c.a] : render::kMaterialCatalog[c.a];
        const bool on = c.kind == Ctrl::FormTab
            ? draft.form == meta.id : draft.material == meta.id;
        brush_->SetColor(on ? gfx::accentC(0.13f)
                            : gfx::ink(hov ? 0.08f : 0.04f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(c.rc, 10.0f, 10.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        brush_->SetColor(on ? gfx::accentC(0.65f)
                            : gfx::ink(hov ? 0.30f : 0.13f));
        dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        const D2D1_RECT_F tb = D2D1::RectF(c.rc.left + 8.0f, c.rc.top + 10.0f,
                                           c.rc.right - 8.0f, c.rc.top + 48.0f);
        drawThumb(dc, c.kind == Ctrl::FormTab ? 0 : 1, meta.thumb, tb,
                  on ? gfx::accentC(1.0f) : gfx::ink(0.50f));
        const D2D1_RECT_F nr = D2D1::RectF(c.rc.left, c.rc.top + 52.0f, c.rc.right,
                                           c.rc.top + 69.0f);
        txt(meta.name, nameFmt_.Get(), nr, gfx::ink(0.90f));
        if (c.kind == Ctrl::MaterialTab && meta.sub) {
          const D2D1_RECT_F sr = D2D1::RectF(c.rc.left, c.rc.top + 72.0f, c.rc.right,
                                             c.rc.top + 88.0f);
          drawTextTrimmed(d3d, meta.sub, smallFmt_.Get(), sr,
                          gfx::ink(0.50f));
        }
        break;
      }
      case Ctrl::CountChip:
      case Ctrl::EdgeChip: {
        const bool on = c.kind == Ctrl::CountChip
            ? draft.count == kCounts[c.a]
            : (c.a == 0 ? draft.edge == "left" : draft.edge == "right");
        brush_->SetColor(on ? gfx::accentC(0.16f)
                            : gfx::ink(hov ? 0.09f : 0.05f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(c.rc, 8.0f, 8.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        brush_->SetColor(on ? gfx::accentC(0.65f)
                            : gfx::ink(hov ? 0.30f : 0.13f));
        dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        const wchar_t* t = c.kind == Ctrl::CountChip
            ? (c.a == 0 ? L"1" : c.a == 1 ? L"3" : c.a == 2 ? L"5" : L"7")
            : (c.a == 0 ? L"左" : L"右");
        brush_->SetColor(gfx::ink(on ? 0.92f : hov ? 0.90f : 0.55f));
        bodyFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);  // chips 居中
        dc->DrawText(t, (UINT32)std::wcslen(t), bodyFmt_.Get(), &c.rc, brush_.Get());
        bodyFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
        break;
      }
      case Ctrl::Gsel: {
        const bool openDrop_ = dropGsel_ == (int)i;
        brush_->SetColor(gfx::ink(hov ? 0.09f : 0.05f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(c.rc, 8.0f, 8.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        brush_->SetColor(openDrop_ ? gfx::accentC(0.60f)
                                   : gfx::ink(hov ? 0.30f : 0.13f));
        dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        const D2D1_RECT_F tr = D2D1::RectF(c.rc.left + 12.0f, c.rc.top,
                                           c.rc.right - 26.0f, c.rc.bottom);
        const std::string v = c.a < (int)draft.mapping.size()
            ? draft.mapping[(size_t)c.a] : "auto";
        drawTextTrimmed(d3d, valueLabel(v), bodyFmt_.Get(), tr, gfx::ink(0.90f));
        // ▾ 用两笔描边画（字形回退不稳，菜单 ✓ 同款策略）
        brush_->SetColor(gfx::ink(0.50f));
        const float ax = c.rc.right - 14.0f;
        const float ay = (c.rc.top + c.rc.bottom) * 0.5f;
        dc->DrawLine(D2D1::Point2F(ax - 3.5f, ay - 1.5f), D2D1::Point2F(ax, ay + 1.8f),
                     brush_.Get(), 1.3f);
        dc->DrawLine(D2D1::Point2F(ax, ay + 1.8f), D2D1::Point2F(ax + 3.5f, ay - 1.5f),
                     brush_.Get(), 1.3f);
        break;
      }
      case Ctrl::MergeSwitch: {
        const bool on = draft.mergeCache;
        brush_->SetColor(on ? gfx::accentC(0.55f) : gfx::ink(0.07f));
        const D2D1_ROUNDED_RECT rr =
            D2D1::RoundedRect(c.rc, kSwitchH * 0.5f, kSwitchH * 0.5f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        brush_->SetColor(on ? gfx::accentC(0.90f) : gfx::ink(0.13f));
        dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        const float kx = on ? c.rc.right - 3.0f - 16.0f : c.rc.left + 3.0f;
        brush_->SetColor(on ? gfx::ink(0.93f) : gfx::ink(0.55f));
        dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(kx + 8.0f, c.rc.top + 11.0f),
                                      8.0f, 8.0f),
                        brush_.Get());
        break;
      }
      default:
        break;
      }
    }
  }
  dc->SetTransform(baseTm);
  dc->PopAxisAlignedClip();

  // 体区滚动条（8px 槽位内 3px 拇指，原型 ::-webkit-scrollbar 近似）
  if (maxScroll() > 0) {
    const float trackH = bodyH() - 8.0f;
    const float th = (std::max)(24.0f, trackH * bodyH() / (float)contentH_);
    const float ty = bodyRc.top + 4.0f +
        (trackH - th) * (float)scrollY / (float)maxScroll();
    brush_->SetColor(gfx::ink(0.25f));
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(
        D2D1::RectF(rect.right - 6.0f, ty, rect.right - 3.0f, ty + th), 1.5f, 1.5f);
    dc->FillRoundedRectangle(&rr, brush_.Get());
  }

  // ── 底部操作条（p-ft）：备注 + 取消 + 保存并生效 ──
  {
    const float ftY = rect.bottom - kFtH;
    hairline(rect.left, ftY + 0.5f, rect.right, ftY + 0.5f);
    smallFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    txt(L"未保存的修改将在关闭时丢弃", smallFmt_.Get(),
        D2D1::RectF(rect.left + 15.0f, ftY, rect.left + 200.0f, rect.bottom),
        gfx::ink(0.50f));
    smallFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);
    auto btn = [&](int ci, const wchar_t* label, bool primary) {
      const Ctrl& c = ctrls_[(size_t)ci];
      const D2D1_RECT_F r = D2D1::RectF(c.rc.left + rect.left, c.rc.top + rect.top,
                                        c.rc.right + rect.left, c.rc.bottom + rect.top);
      const bool hov = hover == ci;
      if (primary) {
        // 原型 btn-primary：accent 80%/黑 底，hover 满 accent
        brush_->SetColor(hov ? gfx::accentC(0.95f)
                             : D2D1::ColorF(0.298f, 0.702f, 0.527f, 0.95f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(r, 8.0f, 8.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        brush_->SetColor(gfx::accentC(0.70f));
        dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        txt(label, btnFmt_.Get(), r, gfx::white(0.97f));
      } else {
        if (hov) {
          brush_->SetColor(gfx::ink(0.06f));
          const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(r, 8.0f, 8.0f);
          dc->FillRoundedRectangle(&rr, brush_.Get());
        }
        brush_->SetColor(gfx::ink(hov ? 0.35f : 0.13f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(r, 8.0f, 8.0f);
        dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        txt(label, btnFmt_.Get(), r, gfx::ink(0.90f));
      }
    };
    btn(1, L"取消", false);
    btn(2, L"保存并生效", true);
  }

  // ── 下拉浮层（.drop）：玻璃底 + 当前项 accent 底 + ✓，内部滚动 ──
  if (dropGsel_ >= 0) {
    dc->SetTransform(D2D1::Matrix3x2F::Translation(rect.left, rect.top) * baseTm);
    material.drawCardBack(dc, dropRc_, 10.0f);
    dc->PushAxisAlignedClip(&dropRc_, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
    const std::string cur = ctrl(dropGsel_).a < (int)draft.mapping.size()
        ? draft.mapping[(size_t)ctrl(dropGsel_).a] : "auto";
    const auto opts = optionsFor(ctrl(dropGsel_).a);
    for (size_t i = dropStart_; i < ctrls_.size(); ++i) {
      const Ctrl& c = ctrls_[i];
      const D2D1_RECT_F r = D2D1::RectF(c.rc.left, c.rc.top - (float)dropScroll_,
                                        c.rc.right, c.rc.bottom - (float)dropScroll_);
      if (r.bottom < dropRc_.top || r.top > dropRc_.bottom) continue;
      const bool on = (size_t)c.b < opts.size() && opts[(size_t)c.b].first == cur;
      const bool hov = (int)i == hover;
      if (on || hov) {
        brush_->SetColor(on ? gfx::accentC(0.18f) : gfx::ink(0.09f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(r, 6.0f, 6.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
      }
      if ((size_t)c.b < opts.size()) {
        const D2D1_RECT_F tr = D2D1::RectF(r.left + 10.0f, r.top, r.right - 26.0f,
                                           r.bottom);
        drawTextTrimmed(d3d, opts[(size_t)c.b].second, bodyFmt_.Get(), tr,
                        gfx::ink(0.90f));
      }
      if (on) {  // ✓ 两笔描边（菜单同款）
        brush_->SetColor(gfx::accentC(1.0f));
        const float cx = r.right - 16.0f;
        const float cy = (r.top + r.bottom) * 0.5f;
        dc->DrawLine(D2D1::Point2F(cx - 4.0f, cy + 0.5f),
                     D2D1::Point2F(cx - 1.0f, cy + 3.5f), brush_.Get(), 1.6f);
        dc->DrawLine(D2D1::Point2F(cx - 1.0f, cy + 3.5f),
                     D2D1::Point2F(cx + 5.0f, cy - 3.5f), brush_.Get(), 1.6f);
      }
    }
    dc->PopAxisAlignedClip();
    dc->SetTransform(baseTm);
  }
}

} // namespace okmeter
