// render/forms/chip.cpp —— 横向 mini chip 文字：名 + 6px + 值，整体居中于项中心；
// 中心项值 accent（原型 .face-mini：k 9.5px ink-dim / v 11px mono ink，center accent）
#include "../form.h"
#include "../material.h"

using Microsoft::WRL::ComPtr;

namespace okmeter::render {

void drawChipText(const DrawContext& ctx, ID2D1DeviceContext* dc, size_t i,
                  float cx, float cy, float alpha) {
  if (!dc || !ctx.d3d || !ctx.geom || !ctx.brush || !ctx.miniNameFmt ||
      !ctx.miniValFmt || !ctx.items || i >= ctx.items->size() ||
      alpha <= 0.003f)
    return;
  const DockItem& di = (*ctx.items)[i];
  auto meas = [&](IDWriteTextFormat* fmt, const std::wstring& s) -> float {
    if (s.empty()) return 0.0f;
    ComPtr<IDWriteTextLayout> tl;
    if (FAILED(ctx.d3d->dwrite()->CreateTextLayout(s.c_str(), (UINT32)s.size(),
                                                   fmt, 4096.0f, 64.0f, &tl)))
      return 0.0f;
    DWRITE_TEXT_METRICS m{};
    return SUCCEEDED(tl->GetMetrics(&m)) ? m.width : 0.0f;
  };
  const float gap = 6.0f * (float)ctx.geom->scale;
  const float nw = meas(ctx.miniNameFmt, di.label);
  const float vw = meas(ctx.miniValFmt, di.value);
  const float total = nw + (nw > 0 && vw > 0 ? gap : 0.0f) + vw;
  float x = cx - total * 0.5f;
  const float sc = (float)ctx.geom->scale;
  const bool center = (int)i == ctx.mid;
  if (nw > 0) {
    ctx.brush->SetColor(inkLight(0.66f * alpha));
    const D2D1_RECT_F tr = D2D1::RectF(x, cy - 8.0f * sc, x + nw + 1.0f, cy + 8.0f * sc);
    dc->DrawText(di.label.c_str(), (UINT32)di.label.size(), ctx.miniNameFmt, &tr,
                 ctx.brush);
    x += nw + gap;
  }
  if (vw > 0) {
    ctx.brush->SetColor(center ? D2D1::ColorF(0x5FE0A8, alpha)
                               : inkLight(0.93f * alpha));
    const D2D1_RECT_F tr = D2D1::RectF(x, cy - 8.5f * sc, x + vw + 1.0f, cy + 8.5f * sc);
    dc->DrawText(di.value.c_str(), (UINT32)di.value.size(), ctx.miniValFmt, &tr,
                 ctx.brush);
  }
}

} // namespace okmeter::render
