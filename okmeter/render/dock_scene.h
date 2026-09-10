// render/dock_scene.h —— dock 场景绘制：弧线 + 球体 + 球内双行文本（暗夜材质）
#pragma once

#include "d3d.h"
#include "../ui/geometry.h"
#include <string>
#include <vector>

namespace okmeter::render {

struct DockItem {
  std::wstring value;  // 紧凑值（球内主文本，Consolas 11px 白 93%）
  std::wstring label;  // 模型短名 / 口径名（Consolas 8.5px 白 66%）
};

// 每帧：弧线连线（hairline 白 13%）→ 球（深玻璃底 + 1px 描边；中心项 accent
// 描边 + 半径+3 accent 10% 光晕环；悬停项按 ItemGeom.scale 绕中心放大）→ 文本。
// e 为滑出进度（0=收缩，1=展开）：收缩时各球心向露出侧收拢（tuck），
// 右缘球心落到 local 12-r（球右帽露出 12px，约半球被屏缘裁掉），左缘镜像。
// 画刷与文本格式是设备相关资源：通过比较 dc 指针检测 D3DContext 设备重建后重建。
class DockScene {
public:
  // g 须先经 applyHover 处理；mid 为中心项下标（accent 高亮）；e=弹簧滑出进度
  void draw(D3DContext& d3d, const DockGeom& g,
            const std::vector<DockItem>& items, int mid,
            double e, const std::string& edge);

private:
  bool ensure(D3DContext& d3d);  // dc 指针变化（设备重建）时重建全部资源

  ID2D1DeviceContext* seen_ = nullptr;
  Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brush_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> valueFmt_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> labelFmt_;
};

} // namespace okmeter::render
