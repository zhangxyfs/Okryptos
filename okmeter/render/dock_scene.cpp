#include "dock_scene.h"

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

constexpr UINT32 kAccent = 0x5FE0A8;  // 原型 accent 绿

D2D1_COLOR_F kHairline() { return D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.13f); }
D2D1_COLOR_F kGlass()    { return D2D1::ColorF(0.06f, 0.07f, 0.09f, 0.92f); }
D2D1_COLOR_F kCardBg()   { return D2D1::ColorF(0.06f, 0.07f, 0.09f, 0.90f); }

// 创建文本格式并设对齐（失败返回 false）
bool makeFmt(IDWriteFactory* dw, const wchar_t* family, float size,
             DWRITE_TEXT_ALIGNMENT halign, IDWriteTextFormat** out) {
  if (FAILED(dw->CreateTextFormat(family, nullptr, DWRITE_FONT_WEIGHT_NORMAL,
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
  if (dc == seen_ && brush_ && valueFmt_ && labelFmt_ && cardTitleFmt_ &&
      cardBigFmt_ && cardRowFmt_ && cardValFmt_ && cardFootFmt_) return true;
  seen_ = dc;
  brush_.Reset();
  valueFmt_.Reset();
  labelFmt_.Reset();
  cardTitleFmt_.Reset();
  cardBigFmt_.Reset();
  cardRowFmt_.Reset();
  cardValFmt_.Reset();
  cardFootFmt_.Reset();
  if (FAILED(dc->CreateSolidColorBrush(kGlass(), &brush_))) return false;
  IDWriteFactory* dw = d3d.dwrite();
  return makeFmt(dw, L"Consolas", 11.0f, DWRITE_TEXT_ALIGNMENT_CENTER, &valueFmt_) &&
         makeFmt(dw, L"Consolas", 8.5f, DWRITE_TEXT_ALIGNMENT_CENTER, &labelFmt_) &&
         makeFmt(dw, L"Segoe UI", 10.5f, DWRITE_TEXT_ALIGNMENT_LEADING, &cardTitleFmt_) &&
         makeFmt(dw, L"Consolas", 21.0f, DWRITE_TEXT_ALIGNMENT_LEADING, &cardBigFmt_) &&
         makeFmt(dw, L"Segoe UI", 11.0f, DWRITE_TEXT_ALIGNMENT_LEADING, &cardRowFmt_) &&
         makeFmt(dw, L"Consolas", 11.0f, DWRITE_TEXT_ALIGNMENT_TRAILING, &cardValFmt_) &&
         makeFmt(dw, L"Segoe UI", 10.0f, DWRITE_TEXT_ALIGNMENT_LEADING, &cardFootFmt_);
}

void DockScene::draw(D3DContext& d3d, const DockGeom& g,
                     const std::vector<DockItem>& items, int mid,
                     double e, const std::string& edge, float dx) {
  if (!ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  const size_t n = g.items.size();
  if (n == 0) return;

  // 收缩 tuck：e=0 时球心收拢到露出条内侧（右帽露出 12px），e=1 回布局位；
  // dx 为球区在窗口内的水平偏移（含卡区时右缘 +268）
  auto drawPos = [&](const ItemGeom& it, D2D1_POINT_2F& out) {
    const double collapsedX =
        edge == "right" ? (12.0 - it.r) : (g.w - 12.0 + it.r);
    out.x = (float)(it.x + (1.0 - e) * (collapsedX - it.x)) + dx;
    out.y = (float)(it.y + it.dy);
  };

  // 弧线：首项到末项依次连线（tuck/dy 随点走）
  brush_->SetColor(kHairline());
  for (size_t i = 0; i + 1 < n; ++i) {
    D2D1_POINT_2F a, b;
    drawPos(g.items[i], a);
    drawPos(g.items[i + 1], b);
    dc->DrawLine(a, b, brush_.Get(), 1.0f);
  }

  for (size_t i = 0; i < n; ++i) {
    const ItemGeom& it = g.items[i];
    D2D1_POINT_2F c;
    drawPos(it, c);
    const float r = (float)it.r;
    const bool isCenter = (int)i == mid;

    const bool scaled = it.scale > 1.001;
    if (scaled)
      dc->SetTransform(D2D1::Matrix3x2F::Scale((float)it.scale, (float)it.scale, c));

    // 中心项：半径+3 accent 10% 光晕环
    if (isCenter) {
      brush_->SetColor(D2D1::ColorF(kAccent, 0.10f));
      const D2D1_ELLIPSE halo = D2D1::Ellipse(c, r + 3.0f, r + 3.0f);
      dc->DrawEllipse(&halo, brush_.Get(), 6.0f);
    }

    // 球体：深玻璃底 + 1px 描边（中心项 accent）
    brush_->SetColor(kGlass());
    const D2D1_ELLIPSE ball = D2D1::Ellipse(c, r, r);
    dc->FillEllipse(&ball, brush_.Get());
    brush_->SetColor(isCenter ? D2D1::ColorF(kAccent, 1.0f) : kHairline());
    dc->DrawEllipse(&ball, brush_.Get(), 1.0f);

    // 球内双行文本：上值下名
    if (i < items.size()) {
      const DockItem& di = items[i];
      if (!di.value.empty()) {
        brush_->SetColor(D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.93f));
        const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y - 15.0f, c.x + r, c.y + 1.0f);
        dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), valueFmt_.Get(),
                     &tr, brush_.Get());
      }
      if (!di.label.empty()) {
        brush_->SetColor(D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.66f));
        const D2D1_RECT_F tr = D2D1::RectF(c.x - r, c.y + 1.0f, c.x + r, c.y + 15.0f);
        dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), labelFmt_.Get(),
                     &tr, brush_.Get());
      }
    }

    if (scaled) dc->SetTransform(D2D1::Matrix3x2F::Identity());
  }
}

void DockScene::drawCard(D3DContext& d3d, const DetailCard& card,
                         const std::string& edge, double ballZoneW,
                         double winH, double anchorY) {
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

  // 卡体：深玻璃底（90% 不透明，v1 无 backdrop blur）+ 1px hairline
  const D2D1_ROUNDED_RECT rr =
      D2D1::RoundedRect(D2D1::RectF(x, y, x + kCardW, y + cardH), 12.0f, 12.0f);
  brush_->SetColor(kCardBg());
  dc->FillRoundedRectangle(&rr, brush_.Get());
  brush_->SetColor(kHairline());
  dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);

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
