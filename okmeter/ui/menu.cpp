#include "menu.h"
#include "../render/materials/glassfx.h"
#include <algorithm>
#include <cmath>

using Microsoft::WRL::ComPtr;

namespace okmeter {
namespace {

// 原型 .ctx 度量：padding 5、按钮 padding 7/10 字号 12.5（行高 28）、
// h6 margin 5/3 字号 10（行 22）、hr margin 5 + 1px 线（行 11）、min-width 184
constexpr float kPad = 5.0f;
constexpr float kItemH = 28.0f;
constexpr float kItemPadX = 10.0f;
constexpr float kTitleH = 22.0f;
constexpr float kSepH = 11.0f;
constexpr float kTickW = 22.0f;    // ✓ 列宽
constexpr float kMinW = 200.0f;    // 简报：宽度按内容（~200px）

float rowH(MenuEntry::Kind k) {
  return k == MenuEntry::Title ? kTitleH
       : k == MenuEntry::Separator ? kSepH : kItemH;
}

} // namespace

bool GlassMenu::ensure(render::D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  if (!dc || !d3d.dwrite()) return false;
  if (dc == seen_ && seenGen_ == d3d.generation() && brush_ && itemFmt_ && titleFmt_)
    return true;
  seen_ = dc;
  seenGen_ = d3d.generation();
  brush_.Reset();
  itemFmt_.Reset();
  titleFmt_.Reset();
  if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush_))) return false;
  IDWriteFactory* dw = d3d.dwrite();
  auto make = [&](float size, IDWriteTextFormat** out) {
    if (FAILED(dw->CreateTextFormat(L"Segoe UI", nullptr, DWRITE_FONT_WEIGHT_NORMAL,
                                    DWRITE_FONT_STYLE_NORMAL, DWRITE_FONT_STRETCH_NORMAL,
                                    size, L"", out)))
      return false;
    (*out)->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    (*out)->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
    return true;
  };
  return make(12.5f, &itemFmt_) && make(10.0f, &titleFmt_);
}

void GlassMenu::layout(render::D3DContext& d3d) {
  float w = kMinW;
  if (ensure(d3d)) {
    for (const MenuEntry& e : entries) {
      if (e.kind == MenuEntry::Separator) continue;
      ComPtr<IDWriteTextLayout> tl;
      IDWriteTextFormat* fmt =
          e.kind == MenuEntry::Title ? titleFmt_.Get() : itemFmt_.Get();
      if (FAILED(d3d.dwrite()->CreateTextLayout(e.label.c_str(), (UINT32)e.label.size(),
                                                fmt, 2000.0f, 100.0f, &tl)))
        continue;
      DWRITE_TEXT_METRICS m{};
      if (FAILED(tl->GetMetrics(&m))) continue;
      const float need = m.width + 2.0f * kItemPadX + 2.0f * kPad +
                         (e.kind == MenuEntry::Item ? kTickW : 0.0f);
      if (need > w) w = need;
    }
  }
  width = (int)std::ceil(w);
  float y = kPad;
  for (MenuEntry& e : entries) {
    e.y0 = y;
    e.y1 = y + rowH(e.kind);
    y = e.y1;
  }
  height = (int)std::ceil(y + kPad);
}

void GlassMenu::place(float x, float y) {
  rect = D2D1::RectF(x, y, x + (float)width, y + (float)height);
}

int GlassMenu::hit(int x, int y) const {
  if (!open || !contains(x, y)) return -1;
  const float ly = (float)y - rect.top;
  for (size_t i = 0; i < entries.size(); ++i) {
    const MenuEntry& e = entries[i];
    if (e.kind != MenuEntry::Item) continue;  // 标题/分隔/灰化不可点
    if (ly >= e.y0 && ly < e.y1) return (int)i;
  }
  return -1;
}

bool GlassMenu::contains(int x, int y) const {
  return (float)x >= rect.left && (float)x < rect.right &&
         (float)y >= rect.top && (float)y < rect.bottom;
}

void GlassMenu::draw(render::D3DContext& d3d, render::IMaterial& material) {
  if (!open || !ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  material.drawCardBack(dc, rect, 10.0f);  // 玻璃底跟随当前材质（圆角 10）

  const float x0 = rect.left + kPad;
  const float x1 = rect.right - kPad;
  for (size_t i = 0; i < entries.size(); ++i) {
    const MenuEntry& e = entries[i];
    const float y0 = rect.top + e.y0;
    const float y1 = rect.top + e.y1;
    if (e.kind == MenuEntry::Separator) {
      brush_->SetColor(render::glassfx::ink(0.13f));  // hairline（原型 .ctx hr）
      const float sy = std::floor((y0 + y1) * 0.5f) + 0.5f;
      dc->DrawLine(D2D1::Point2F(x0 + 4.0f, sy), D2D1::Point2F(x1 - 4.0f, sy),
                   brush_.Get(), 1.0f);
      continue;
    }
    if (e.kind == MenuEntry::Title) {
      brush_->SetColor(render::glassfx::ink(0.50f));
      const D2D1_RECT_F tr = D2D1::RectF(x0 + kItemPadX, y0, x1 - kItemPadX, y1);
      dc->DrawText(e.label.c_str(), (UINT32)e.label.size(), titleFmt_.Get(), &tr,
                   brush_.Get());
      continue;
    }
    // Item / Disabled：悬停高亮 ink 9% 圆角 6（原型 .ctx button:hover）
    if (e.kind == MenuEntry::Item && (int)i == hover) {
      brush_->SetColor(render::glassfx::ink(0.09f));
      const D2D1_ROUNDED_RECT rr =
          D2D1::RoundedRect(D2D1::RectF(x0, y0 + 1.0f, x1, y1 - 1.0f), 6.0f, 6.0f);
      dc->FillRoundedRectangle(&rr, brush_.Get());
    }
    brush_->SetColor(render::glassfx::ink(e.kind == MenuEntry::Disabled ? 0.35f : 0.90f));
    const D2D1_RECT_F tr =
        D2D1::RectF(x0 + kItemPadX, y0, x1 - kItemPadX - kTickW, y1);
    dc->DrawText(e.label.c_str(), (UINT32)e.label.size(), itemFmt_.Get(), &tr,
                 brush_.Get());
    if (e.tick) {
      // ✓ 用描边画（Segoe UI 字形回退不稳，两笔更脆且与原型 tick 同义）
      brush_->SetColor(render::glassfx::accentC(1.0f));
      const float cx = x1 - kItemPadX - 6.0f;
      const float cy = (y0 + y1) * 0.5f;
      dc->DrawLine(D2D1::Point2F(cx - 4.0f, cy + 0.5f),
                   D2D1::Point2F(cx - 1.0f, cy + 3.5f), brush_.Get(), 1.6f);
      dc->DrawLine(D2D1::Point2F(cx - 1.0f, cy + 3.5f),
                   D2D1::Point2F(cx + 5.0f, cy - 3.5f), brush_.Get(), 1.6f);
    }
  }
}

} // namespace okmeter
