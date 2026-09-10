#include "dock_scene.h"

using Microsoft::WRL::ComPtr;

namespace okmeter::render {
namespace {

constexpr UINT32 kAccent = 0x5FE0A8;  // 原型 accent 绿

D2D1_COLOR_F kHairline() { return D2D1::ColorF(1.0f, 1.0f, 1.0f, 0.13f); }
D2D1_COLOR_F kGlass()    { return D2D1::ColorF(0.06f, 0.07f, 0.09f, 0.92f); }

} // namespace

bool DockScene::ensure(D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  if (!dc || !d3d.dwrite()) return false;
  if (dc == seen_ && brush_ && valueFmt_ && labelFmt_) return true;
  seen_ = dc;
  brush_.Reset();
  valueFmt_.Reset();
  labelFmt_.Reset();
  if (FAILED(dc->CreateSolidColorBrush(kGlass(), &brush_))) return false;
  IDWriteFactory* dw = d3d.dwrite();
  if (FAILED(dw->CreateTextFormat(L"Consolas", nullptr, DWRITE_FONT_WEIGHT_NORMAL,
                                  DWRITE_FONT_STYLE_NORMAL, DWRITE_FONT_STRETCH_NORMAL,
                                  11.0f, L"", &valueFmt_))) return false;
  if (FAILED(dw->CreateTextFormat(L"Consolas", nullptr, DWRITE_FONT_WEIGHT_NORMAL,
                                  DWRITE_FONT_STYLE_NORMAL, DWRITE_FONT_STRETCH_NORMAL,
                                  8.5f, L"", &labelFmt_))) return false;
  valueFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);
  valueFmt_->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
  labelFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);
  labelFmt_->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
  return true;
}

void DockScene::draw(D3DContext& d3d, const DockGeom& g,
                     const std::vector<DockItem>& items, int mid,
                     double e, const std::string& edge) {
  if (!ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  const size_t n = g.items.size();
  if (n == 0) return;

  // 收缩 tuck：e=0 时球心收拢到露出条内侧（右帽露出 12px），e=1 回布局位
  auto drawPos = [&](const ItemGeom& it, D2D1_POINT_2F& out) {
    const double collapsedX =
        edge == "right" ? (12.0 - it.r) : (g.w - 12.0 + it.r);
    out.x = (float)(it.x + (1.0 - e) * (collapsedX - it.x));
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

} // namespace okmeter::render
