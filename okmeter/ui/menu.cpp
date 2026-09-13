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
constexpr float kTickW = 22.0f;    // ✓ / ▸ 列宽
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

void GlassMenu::layoutCol(render::D3DContext& d3d, MenuColumn& col) {
  float w = kMinW;
  if (ensure(d3d)) {
    for (const MenuEntry& e : col.entries) {
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
                         (e.kind == MenuEntry::Item || e.kind == MenuEntry::Parent
                              ? kTickW : 0.0f);
      if (need > w) w = need;
    }
  }
  col.width = (int)std::ceil(w);
  float y = kPad;
  for (MenuEntry& e : col.entries) {
    e.y0 = y;
    e.y1 = y + rowH(e.kind);
    y = e.y1;
  }
  col.height = (int)std::ceil(y + kPad);
}

void GlassMenu::layout(render::D3DContext& d3d) {
  MenuColumn main{ entries, {}, width, height, hover };
  layoutCol(d3d, main);
  width = main.width;
  height = main.height;
  entries = std::move(main.entries);
  if (!sub1.entries.empty()) layoutCol(d3d, sub1);
  if (!sub2.entries.empty()) layoutCol(d3d, sub2);
}

void GlassMenu::place(float x, float y) {
  rect = D2D1::RectF(x, y, x + (float)width, y + (float)height);
}

void GlassMenu::placeSub(MenuColumn& col, float x, float y) {
  col.rect = D2D1::RectF(x, y, x + (float)col.width, y + (float)col.height);
}

namespace {
int hitCol(const MenuColumn& col, int x, int y, int colIdx) {
  if (col.entries.empty()) return -1;
  if ((float)x < col.rect.left || (float)x >= col.rect.right ||
      (float)y < col.rect.top || (float)y >= col.rect.bottom)
    return -1;
  const float ly = (float)y - col.rect.top;
  for (size_t i = 0; i < col.entries.size(); ++i) {
    const MenuEntry& e = col.entries[i];
    if (e.kind != MenuEntry::Item && e.kind != MenuEntry::Parent) continue;
    if (ly >= e.y0 && ly < e.y1) return colIdx * 1000 + (int)i;
  }
  return -2;  // 列内但不可点行（标题/分隔）
}
} // namespace

int GlassMenu::hit(int x, int y) const {
  if (!open) return -1;
  // 子列优先（后开的在上层）
  int h = hitCol(sub2, x, y, 2);
  if (h != -1) return h;
  h = hitCol(sub1, x, y, 1);
  if (h != -1) return h;
  if ((float)x >= rect.left && (float)x < rect.right &&
      (float)y >= rect.top && (float)y < rect.bottom) {
    const float ly = (float)y - rect.top;
    for (size_t i = 0; i < entries.size(); ++i) {
      const MenuEntry& e = entries[i];
      if (e.kind != MenuEntry::Item && e.kind != MenuEntry::Parent) continue;
      if (ly >= e.y0 && ly < e.y1) return (int)i;
    }
    return -2;
  }
  return -1;
}

bool GlassMenu::contains(int x, int y) const {
  const auto in = [](const D2D1_RECT_F& r, float px, float py) {
    return r.right > r.left && px >= r.left && px < r.right &&
           py >= r.top && py < r.bottom;
  };
  return in(rect, (float)x, (float)y) || in(sub1.rect, (float)x, (float)y) ||
         in(sub2.rect, (float)x, (float)y);
}

D2D1_RECT_F GlassMenu::bounds() const {
  D2D1_RECT_F b = rect;
  const auto u = [&b](const D2D1_RECT_F& r) {
    if (r.right <= r.left) return;
    b.left = (std::min)(b.left, r.left);
    b.top = (std::min)(b.top, r.top);
    b.right = (std::max)(b.right, r.right);
    b.bottom = (std::max)(b.bottom, r.bottom);
  };
  u(sub1.rect);
  u(sub2.rect);
  return b;
}

void GlassMenu::drawCol(render::D3DContext& d3d, render::IMaterial& material,
                        MenuColumn& col, int hoverIdx) {
  if (col.entries.empty()) return;
  ID2D1DeviceContext* dc = d3d.dc();
  material.drawCardBack(dc, col.rect, 10.0f);  // 玻璃底跟随当前材质（圆角 10）

  const float x0 = col.rect.left + kPad;
  const float x1 = col.rect.right - kPad;
  for (size_t i = 0; i < col.entries.size(); ++i) {
    const MenuEntry& e = col.entries[i];
    const float y0 = col.rect.top + e.y0;
    const float y1 = col.rect.top + e.y1;
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
    // Item / Parent / Disabled：悬停高亮 ink 9% 圆角 6（原型 .ctx button:hover）
    if (e.kind != MenuEntry::Disabled && (int)i == hoverIdx) {
      brush_->SetColor(render::glassfx::ink(0.09f));
      const D2D1_ROUNDED_RECT rr =
          D2D1::RoundedRect(D2D1::RectF(x0, y0 + 1.0f, x1, y1 - 1.0f), 6.0f, 6.0f);
      dc->FillRoundedRectangle(&rr, brush_.Get());
    }
    brush_->SetColor(render::glassfx::ink(e.kind == MenuEntry::Disabled ? 0.35f : 0.90f));
    // 向左展开的父项：◂ 占行首 kTickW，文本右移；其余文本贴行首、行尾留给 ✓/▸
    const bool leftArrow = e.kind == MenuEntry::Parent && subDir < 0;
    const float tx = x0 + kItemPadX + (leftArrow ? kTickW : 0.0f);
    const D2D1_RECT_F tr = D2D1::RectF(tx, y0, x1 - kItemPadX - kTickW, y1);
    dc->DrawText(e.label.c_str(), (UINT32)e.label.size(), itemFmt_.Get(), &tr,
                 brush_.Get());
    if (e.kind == MenuEntry::Parent) {
      // 展开指示随子列方向：向右 ▸ 于行尾，向左 ◂ 于行首（两笔描边）
      brush_->SetColor(render::glassfx::ink(0.50f));
      const float cy = (y0 + y1) * 0.5f;
      if (subDir < 0) {
        const float cx = x0 + kItemPadX + 6.0f;
        dc->DrawLine(D2D1::Point2F(cx + 1.5f, cy - 4.5f),
                     D2D1::Point2F(cx - 3.5f, cy), brush_.Get(), 1.6f);
        dc->DrawLine(D2D1::Point2F(cx - 3.5f, cy),
                     D2D1::Point2F(cx + 1.5f, cy + 4.5f), brush_.Get(), 1.6f);
      } else {
        const float cx = x1 - kItemPadX - 6.0f;
        dc->DrawLine(D2D1::Point2F(cx - 1.5f, cy - 4.5f),
                     D2D1::Point2F(cx + 3.5f, cy), brush_.Get(), 1.6f);
        dc->DrawLine(D2D1::Point2F(cx + 3.5f, cy),
                     D2D1::Point2F(cx - 1.5f, cy + 4.5f), brush_.Get(), 1.6f);
      }
    } else if (e.tick) {
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

void GlassMenu::draw(render::D3DContext& d3d, render::IMaterial& material) {
  if (!open || !ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);
  MenuColumn main{ entries, rect, width, height, hover };
  drawCol(d3d, material, main, hover);
  drawCol(d3d, material, sub1, sub1.hover);
  drawCol(d3d, material, sub2, sub2.hover);
}

} // namespace okmeter
