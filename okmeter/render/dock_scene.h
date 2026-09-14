// render/dock_scene.h —— dock 场景编排：D2D 资源缓存（画刷/文本格式）+ 收缩 tuck
// 位置烘焙 + 形态/材质委托 + 悬停详情卡。球底/卡底/弧线见 render/material.h，
// 项布局与球内文本见 render/form.h。
#pragma once

#include "form.h"
#include "material.h"
#include <string>
#include <utility>
#include <vector>

namespace okmeter::render {

// 悬停详情卡内容（app 侧在 hoverIdx 变化/数据刷新时重组，非每帧）
struct DetailCard {
  std::wstring title;  // 模型 id / "口径 · 总量模式"（Segoe UI 10.5px 白 55%）
  std::wstring big;    // 精确累计（Consolas 21px 千分位 白 95%）
  std::vector<std::pair<std::wstring, std::wstring>> rows;  // label dim + 值 mono 右对齐
  std::wstring foot;   // 模型模式："最近调用 … · 模型模式"；总量模式留空
  bool valid = false;
};

// 每帧：烘焙最终位置（e 收缩 tuck + 悬停让位 dy + 卡区偏移 dx 并入 geom）→
// material.drawArcStroke → form.drawItems（球底委托 material.drawOrbBack）。
// 画刷与文本格式是设备相关资源：比较 dc 指针 + 设备代际检测 D3DContext 重建后重建。
class DockScene {
public:
  // g 须先经 applyHover 处理；mid 为中心项下标（accent 高亮）；e=弹簧滑出进度；
  // dx 为球区整体水平偏移（窗口含卡区时右缘 +268，球区贴屏缘不动）；
  // backdropDX/DY 为背景纹理→窗口坐标平移（材质玻璃取样对齐用）；
  // pressIdx ≥0 时该项烘焙 scale×0.9（沉浸光感按压下沉，原型 .pressed 同款）
  void draw(D3DContext& d3d, IForm& form, IMaterial& material,
            BackdropCapture* backdrop, const DockGeom& g,
            const std::vector<DockItem>& items, int mid,
            double e, const std::string& edge, float dx = 0,
            float backdropDX = 0, float backdropDY = 0, int pressIdx = -1,
            const DockGeom* mini = nullptr);

  // 横向 mini chip 实测宽（名+值文本测量 + 内边距 11×2 + 间距 6，全 ×fmtScale）；
  // 返回空 = 测量不可用（格式未建/设备未就绪），调用方回退 96×scale 估值
  std::vector<double> measureChipWidths(D3DContext& d3d,
                                        const std::vector<DockItem>& items);

  // 悬停详情卡：宽 252、圆角 12、卡底委托 material.drawCardBack；竖向水平位置按原型
  // RADII 规则（卡内缘 = 悬停项内缘 ∓ (cardRadius+12)），并夹取到不遮挡任何项
  //（项列最内缘再让 12px）；垂直居中 anchorY 并夹进 [12, winH-12]。
  // 横向：水平居中于悬停项并夹进 [8, winW-cardW-8]；垂直 edge=top 在条区之下
  //（y = winH-kCardZoneH+12，截底保头 availH = kCardZoneH-24），edge=bottom 在条区
  // 之上贴条（y = kCardZoneH-12-drawH）。
  // g 为经 applyHover 的布局几何（未烘焙 tuck），dx 为球区整体水平偏移。
  void drawCard(D3DContext& d3d, IMaterial& material, const DetailCard& card,
                const std::string& edge, const DockGeom& g, float dx, double winW,
                double winH, int hoverIdx, double cardRadius);

private:
  bool ensure(D3DContext& d3d, float scale = 1.0f);  // dc 指针/代际/比例变化时重建全部资源
  float fmtScale_ = -1.0f;  // 当前文本格式的比例（-1=未建；uiScale 变化触发重建）

  ID2D1DeviceContext* seen_ = nullptr;
  unsigned seenGen_ = 0;  // 上次重建资源时的设备代际（防 dc 地址复用 ABA 误判）
  Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brush_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> valueFmt_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> labelFmt_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> capNameFmt_;  // 胶囊左名 Segoe UI 10 左
  Microsoft::WRL::ComPtr<IDWriteTextFormat> capValFmt_;   // 胶囊右值 Consolas 11 半粗右
  Microsoft::WRL::ComPtr<IDWriteTextFormat> hubFmt_;      // 罗盘中心值 Consolas 15
  Microsoft::WRL::ComPtr<IDWriteTextFormat> satFmt_;      // 罗盘卫星值 Consolas 9.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardTitleFmt_;  // Segoe UI 10.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardBigFmt_;    // Consolas 21
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardRowFmt_;    // Segoe UI 11 左对齐
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardValFmt_;    // Consolas 11 右对齐
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardFootFmt_;   // Segoe UI 10
  Microsoft::WRL::ComPtr<IDWriteTextFormat> miniNameFmt_;  // chip 名 Segoe UI 9.5 左
  Microsoft::WRL::ComPtr<IDWriteTextFormat> miniValFmt_;   // chip 值 Consolas 11 左
};

} // namespace okmeter::render
