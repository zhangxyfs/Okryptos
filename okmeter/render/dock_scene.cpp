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
      labelFmt_ && capNameFmt_ && capValFmt_ && hubFmt_ && satFmt_ &&
      cardTitleFmt_ && cardBigFmt_ && cardRowFmt_ && cardValFmt_ &&
      cardFootFmt_) return true;
  seen_ = dc;
  seenGen_ = d3d.generation();
  brush_.Reset();
  valueFmt_.Reset();
  labelFmt_.Reset();
  capNameFmt_.Reset();
  capValFmt_.Reset();
  hubFmt_.Reset();
  satFmt_.Reset();
  cardTitleFmt_.Reset();
  cardBigFmt_.Reset();
  cardRowFmt_.Reset();
  cardValFmt_.Reset();
  cardFootFmt_.Reset();
  if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush_))) return false;
  IDWriteFactory* dw = d3d.dwrite();
  // 数值字号校准原型：值 13px 600 字重、短名 8.5px；
  // 胶囊左名 10px / 右值 11px 600、罗盘中心 15px / 卫星 9.5px（原型 .cap/.hub/.sat）
  return makeFmt(dw, L"Consolas", 13.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD,
                 DWRITE_TEXT_ALIGNMENT_CENTER, &valueFmt_) &&
         makeFmt(dw, L"Consolas", 8.5f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_CENTER, &labelFmt_) &&
         makeFmt(dw, L"Segoe UI", 10.0f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_LEADING, &capNameFmt_) &&
         makeFmt(dw, L"Consolas", 11.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD,
                 DWRITE_TEXT_ALIGNMENT_TRAILING, &capValFmt_) &&
         makeFmt(dw, L"Consolas", 15.0f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_CENTER, &hubFmt_) &&
         makeFmt(dw, L"Consolas", 9.5f, DWRITE_FONT_WEIGHT_NORMAL,
                 DWRITE_TEXT_ALIGNMENT_CENTER, &satFmt_) &&
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
                     float backdropDX, float backdropDY, int pressIdx) {
  if (!ensure(d3d)) return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  const size_t n = g.items.size();
  if (n == 0) return;

  // 位置烘焙：悬停让位 dy + 卡区偏移 dx 并入 geom，形态/材质直读最终坐标。
  // 收缩不再 tuck 项心（原型收缩 = 整个 dock 平移露出左半 50%，项位置不动；
  // 旧"帽露出 12px"机制与 50% 露出叠加会导致只见帽尖不见左半——实测回归）
  DockGeom baked = g;
  for (size_t i = 0; i < n; ++i) {
    const ItemGeom& it = g.items[i];
    baked.items[i].x = it.x + dx;
    baked.items[i].y = it.y + it.dy;
    if ((int)i == pressIdx) baked.items[i].scale *= 0.9;  // 按压下沉（球与文本同步）
  }

  material.drawArcStroke(dc, baked, edge);

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
  ctx.capNameFmt = capNameFmt_.Get();
  ctx.capValFmt = capValFmt_.Get();
  ctx.hubFmt = hubFmt_.Get();
  ctx.satFmt = satFmt_.Get();
  ctx.backdrop = backdrop;
  ctx.backdropDX = backdropDX;
  ctx.backdropDY = backdropDY;
  form.drawItems(dc, ctx);
}

void DockScene::drawCard(D3DContext& d3d, IMaterial& material,
                         const DetailCard& card, const std::string& edge,
                         const DockGeom& g, float dx, double winH,
                         int hoverIdx, double cardRadius) {
  if (!card.valid || !ensure(d3d) || hoverIdx < 0 ||
      hoverIdx >= (int)g.items.size())
    return;
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  constexpr float kCardW = 252.0f;
  constexpr float kCardZoneW = 268.0f;  // 展开态卡区宽（app.cpp kCardZoneW 同款）
  constexpr float kPadX = 16.0f, kPadTop = 14.0f, kPadBot = 12.0f;
  constexpr float kTitleH = 15.0f, kBigH = 27.0f, kRowH = 22.0f;
  const float footH = card.foot.empty() ? 0.0f : 22.0f;  // 8 间距 + 14 行高
  const float cardH = kPadTop + kTitleH + 3.0f + kBigH + 10.0f +
                      kRowH * (float)card.rows.size() + footH + kPadBot;

  // 水平定位（原型 hotSlot 的 RADII 规则）：卡内缘 = 悬停项中心 ∓ (cardRadius+12)；
  // 再夹到项列最内缘 ∓12（规格 §3.4 不遮挡其他球），最后夹进窗口 [8, g.w+268-252-8]
  const ItemGeom& it = g.items[(size_t)hoverIdx];
  double colMin = 1e9, colMax = -1e9;
  for (const ItemGeom& o : g.items) {
    const double half = o.hw > 0 ? o.hw : o.r;
    if (o.x + dx - half < colMin) colMin = o.x + dx - half;
    if (o.x + dx + half > colMax) colMax = o.x + dx + half;
  }
  float x;
  if (edge == "right") {
    const double desired = it.x + dx - cardRadius - 12.0 - kCardW;
    const double limit = colMin - 12.0 - kCardW;
    x = (float)(desired < limit ? desired : limit);  // windows.h min/max 宏冲突，手写比较
    if (x < 8.0f) x = 8.0f;
  } else {
    const double desired = it.x + dx + cardRadius + 12.0;
    const double limit = colMax + 12.0;
    x = (float)(desired > limit ? desired : limit);
    const float maxX = (float)g.w + kCardZoneW - kCardW - 8.0f;
    if (x > maxX) x = maxX;
  }
  const double anchorY = it.y + it.dy;
  // 垂直夹取负 maxY 兜底：窗口装不下整卡（winH-24 < cardH → maxY<12）时截底保头——
  // 卡高收到 winH-24（超出的尾部行整行弃画，标题/大数必可见），maxY 恒 ≥12
  const float availH = (float)winH - 24.0f;
  const float drawH = cardH > availH ? availH : cardH;
  float y = (float)anchorY - drawH * 0.5f;
  const float maxY = (float)winH - drawH - 12.0f;
  if (y > maxY) y = maxY;
  if (y < 12.0f) y = 12.0f;

  material.drawCardBack(dc, D2D1::RectF(x, y, x + kCardW, y + drawH), 12.0f);

  const float cx0 = x + kPadX;
  const float cx1 = x + kCardW - kPadX;
  const float contentBot = y + drawH - 4.0f;  // 截底卡的可见下界（行整行弃画判定）
  float ty = y + kPadTop;
  auto text = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                  float top, float bot, D2D1_COLOR_F color) {
    if (s.empty() || bot > contentBot) return;
    brush_->SetColor(color);
    const D2D1_RECT_F tr = D2D1::RectF(cx0, top, cx1, bot);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &tr, brush_.Get());
  };

  // 标题：长模型 id 省略号裁剪（DrawText 无裁剪能力；CreateTextLayout +
  // 逐字符 trimming 省略号，原型 .d-name text-overflow:ellipsis 同款）
  const float cardLuma = material.backdropLuma();  // 亮背景自适应墨色
  if (!card.title.empty() && ty + kTitleH <= contentBot) {
    ComPtr<IDWriteTextLayout> tl;
    if (SUCCEEDED(d3d.dwrite()->CreateTextLayout(
            card.title.c_str(), (UINT32)card.title.size(), cardTitleFmt_.Get(),
            cx1 - cx0, kTitleH, &tl))) {
      DWRITE_TRIMMING trim{DWRITE_TRIMMING_GRANULARITY_CHARACTER, 0, 0};
      (void)tl->SetTrimming(&trim, nullptr);
      (void)tl->SetWordWrapping(DWRITE_WORD_WRAPPING_NO_WRAP);
      brush_->SetColor(inkOn(cardLuma, 0.55f));
      dc->DrawTextLayout(D2D1::Point2F(cx0, ty), tl.Get(), brush_.Get());
    }
  }
  ty += kTitleH + 3.0f;
  text(card.big, cardBigFmt_.Get(), ty, ty + kBigH,
       inkOn(cardLuma, 0.95f));
  ty += kBigH + 10.0f;

  for (const auto& row : card.rows) {
    if (ty + kRowH > contentBot) break;  // 截底卡：放不下的行整行弃画
    brush_->SetColor(inkOn(cardLuma, 0.07f));  // 行间分隔 hairline
    dc->DrawLine(D2D1::Point2F(cx0, ty), D2D1::Point2F(cx1, ty), brush_.Get(), 1.0f);
    text(row.first, cardRowFmt_.Get(), ty, ty + kRowH,
         inkOn(cardLuma, 0.55f));
    text(row.second, cardValFmt_.Get(), ty, ty + kRowH,
         inkOn(cardLuma, 0.93f));
    ty += kRowH;
  }

  if (!card.foot.empty())
    text(card.foot, cardFootFmt_.Get(), ty + 8.0f, ty + footH,
         inkOn(cardLuma, 0.45f));
}

} // namespace okmeter::render
