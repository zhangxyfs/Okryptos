#include "dock_scene.h"

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

// 创建文本格式并设对齐（失败返回 false）
bool makeFmt(IDWriteFactory* dw, const wchar_t* family, float size,
             DWRITE_FONT_WEIGHT weight, DWRITE_TEXT_ALIGNMENT halign,
             IDWriteTextFormat** out) {
  if (FAILED(dw->CreateTextFormat(family, nullptr, weight,
                                  DWRITE_FONT_STYLE_NORMAL, DWRITE_FONT_STRETCH_NORMAL,
                                  size, L"", out))) return false;
  (*out)->SetTextAlignment(halign);
  (*out)->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
  return true;
}

} // namespace

bool DockScene::ensure(D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  if (!dc || !d3d.dwrite()) return false;
  if (dc == seen_ && seenGen_ == d3d.generation() && brush_ && valueFmt_ &&
      labelFmt_ && cardTitleFmt_ && cardBigFmt_ && cardRowFmt_ && cardValFmt_ &&
      cardFootFmt_) return true;
  seen_ = dc;
  seenGen_ = d3d.generation();
  brush_.Reset();
  valueFmt_.Reset();
  labelFmt_.Reset();
  cardTitleFmt_.Reset();
  cardBigFmt_.Reset();
  cardRowFmt_.Reset();
  cardValFmt_.Reset();
  cardFootFmt_.Reset();
  if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush_))) return false;
  IDWriteFactory* dw = d3d.dwrite();
  // 数值字号校准原型：值 13px 600 字重、短名 8.5px
  return makeFmt(dw, L"Consolas", 13.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD,
                 DWRITE_TEXT_ALIGNMENT_CENTER, &valueFmt_) &&
         makeFmt(dw, L"Consolas", 8.5f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_CENTER, &labelFmt_) &&
         makeFmt(dw, L"Segoe UI", 10.5f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_LEADING, &cardTitleFmt_) &&
         makeFmt(dw, L"Consolas", 21.0f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_LEADING, &cardBigFmt_) &&
         makeFmt(dw, L"Segoe UI", 11.0f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_LEADING, &cardRowFmt_) &&
         makeFmt(dw, L"Consolas", 11.0f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_TRAILING, &cardValFmt_) &&
         makeFmt(dw, L"Segoe UI", 10.0f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_LEADING, &cardFootFmt_);
}

void DockScene::draw(D3DContext& d3d, IForm& form, IMaterial& material,
                     BackdropCapture* backdrop, const DockGeom& g,
                     const std::vector<DockItem>& items, int mid,
                     double e, const std::string& edge, float dx,
                     float backdropDX, float backdropDY) {
  if (!ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  const size_t n = g.items.size();
  if (n == 0) return;

  // 位置烘焙：收缩 tuck（e=0 球心收拢到露出侧，球帽露出 kCollapsedCapPx）+
  // 悬停让位 dy + 卡区偏移 dx，全部并入 geom，形态/材质直读最终坐标
  DockGeom baked = g;
  for (size_t i = 0; i < n; ++i) {
    const ItemGeom& it = g.items[i];
    const double collapsedX =
        edge == "right" ? ((double)kCollapsedCapPx - it.r)
                        : (g.w - kCollapsedCapPx + it.r);
    baked.items[i].x = it.x + (1.0 - e) * (collapsedX - it.x) + dx;
    baked.items[i].y = it.y + it.dy;
  }

  material.drawArcStroke(dc, baked);

  DrawContext ctx;
  ctx.d3d = &d3d;
  ctx.material = &material;
  ctx.geom = &baked;
  ctx.items = &items;
  ctx.mid = mid;
  ctx.e = e;
  ctx.edge = edge;
  ctx.brush = brush_.Get();
  ctx.valueFmt = valueFmt_.Get();
  ctx.labelFmt = labelFmt_.Get();
  ctx.backdrop = backdrop;
  ctx.backdropDX = backdropDX;
  ctx.backdropDY = backdropDY;
  form.drawItems(dc, ctx);
}

void DockScene::drawCard(D3DContext& d3d, IMaterial& material,
                         const DetailCard& card, const std::string& edge,
                         double ballZoneW, double winH, double anchorY) {
  if (!card.valid || !ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  constexpr float kCardW = 252.0f;
  constexpr float kPadX = 16.0f, kPadTop = 14.0f, kPadBot = 12.0f;
  constexpr float kTitleH = 15.0f, kBigH = 27.0f, kRowH = 22.0f;
  const float footH = card.foot.empty() ? 0.0f : 22.0f;  // 8 间距 + 14 行高
  const float cardH = kPadTop + kTitleH + 3.0f + kBigH + 10.0f +
                      kRowH * (float)card.rows.size() + footH + kPadBot;

  // 卡区在球区屏内侧：右缘靠左（x=8），左缘镜像（球区右侧）
  const float x = edge == "right" ? 8.0f : (float)ballZoneW + 8.0f;
  float y = (float)anchorY - cardH * 0.5f;
  const float maxY = (float)winH - cardH - 12.0f;
  if (y > maxY) y = maxY;
  if (y < 12.0f) y = 12.0f;

  material.drawCardBack(dc, D2D1::RectF(x, y, x + kCardW, y + cardH), 12.0f);

  const float cx0 = x + kPadX;
  const float cx1 = x + kCardW - kPadX;
  float ty = y + kPadTop;
  auto text = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                  float top, float bot, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    const D2D1_RECT_F tr = D2D1::RectF(cx0, top, cx1, bot);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &tr, brush_.Get());
  };

  text(card.title, cardTitleFmt_.Get(), ty, ty + kTitleH,
       D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.55f));
  ty += kTitleH + 3.0f;
  text(card.big, cardBigFmt_.Get(), ty, ty + kBigH,
       D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.95f));
  ty += kBigH + 10.0f;

  for (const auto& row : card.rows) {
    brush_->SetColor(D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.07f));  // 行间分隔 hairline
    dc->DrawLine(D2D1::Point2F(cx0, ty), D2D1::Point2F(cx1, ty), brush_.Get(), 1.0f);
    text(row.first, cardRowFmt_.Get(), ty, ty + kRowH,
         D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.55f));
    text(row.second, cardValFmt_.Get(), ty, ty + kRowH,
         D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.93f));
    ty += kRowH;
  }

  if (!card.foot.empty())
    text(card.foot, cardFootFmt_.Get(), ty + 8.0f, ty + footH,
         D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.45f));
}

} // namespace okmeter::render
