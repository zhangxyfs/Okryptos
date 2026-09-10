// render/dock_scene.h —— dock 场景绘制：弧线 + 球体 + 球内双行文本 + 悬停详情卡（暗夜材质）
#pragma once

#include "d3d.h"
#include "../ui/geometry.h"
#include <string>
#include <utility>
#include <vector>

namespace okmeter::render {

struct DockItem {
  std::wstring value;  // 紧凑值（球内主文本，Consolas 11px 白 93%）
  std::wstring label;  // 模型短名 / 口径名（Consolas 8.5px 白 66%）
};

// 悬停详情卡内容（app 侧在 hoverIdx 变化/数据刷新时重组，非每帧）
struct DetailCard {
  std::wstring title;  // 模型 id / "口径 · 总量模式"（Segoe UI 10.5px 白 55%）
  std::wstring big;    // 精确累计（Consolas 21px 千分位 白 95%）
  std::vector<std::pair<std::wstring, std::wstring>> rows;  // label dim + 值 mono 右对齐
  std::wstring foot;   // 模型模式："最近调用 … · 模型模式"；总量模式留空
  bool valid = false;
};

// 每帧：弧线连线（hairline 白 13%）→ 球（深玻璃底 + 1px 描边；中心项 accent
// 描边 + 半径+3 accent 10% 光晕环；悬停项按 ItemGeom.scale 绕中心放大）→ 文本。
// e 为滑出进度（0=收缩，1=展开）：收缩时各球心向露出侧收拢（tuck），
// 右缘球心落到 local 12-r（球右帽露出 12px，约半球被屏缘裁掉），左缘镜像。
// 画刷与文本格式是设备相关资源：通过比较 dc 指针检测 D3DContext 设备重建后重建。
class DockScene {
public:
  // g 须先经 applyHover 处理；mid 为中心项下标（accent 高亮）；e=弹簧滑出进度；
  // dx 为球区整体水平偏移（窗口含卡区时右缘 +268，球区贴屏缘不动）
  void draw(D3DContext& d3d, const DockGeom& g,
            const std::vector<DockItem>& items, int mid,
            double e, const std::string& edge, float dx = 0);

  // 悬停详情卡：宽 252、圆角 12、90% 不透明深底 + 1px hairline；位于球区屏内侧
  // （右缘：卡区在左 x∈[8,260]；左缘镜像 x=ballZoneW+8）；垂直居中 anchorY 并
  // 夹进 [12, winH-12]。ballZoneW = layoutArc g.w，winH = 窗口高。
  void drawCard(D3DContext& d3d, const DetailCard& card, const std::string& edge,
                double ballZoneW, double winH, double anchorY);

private:
  bool ensure(D3DContext& d3d);  // dc 指针变化（设备重建）时重建全部资源

  ID2D1DeviceContext* seen_ = nullptr;
  Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brush_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> valueFmt_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> labelFmt_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardTitleFmt_;  // Segoe UI 10.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardBigFmt_;    // Consolas 21
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardRowFmt_;    // Segoe UI 11 左对齐
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardValFmt_;    // Consolas 11 右对齐
  Microsoft::WRL::ComPtr<IDWriteTextFormat> cardFootFmt_;   // Segoe UI 10
};

} // namespace okmeter::render
