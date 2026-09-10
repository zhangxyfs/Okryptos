// render/form.h —— 形态接口（IForm）：布局 + 项绘制。形态只管"球/项在哪、项内画什么"，
// 球底/卡底/弧线描边等皮肤行为委托给 IMaterial（render/material.h）。
#pragma once

#include "d3d.h"
#include "../core/registry.h"
#include "../ui/geometry.h"
#include <string>
#include <vector>

namespace okmeter::render {

class IMaterial;
class BackdropCapture;

struct DockItem {
  std::wstring value;  // 紧凑值（球内主文本，Consolas 13px 半粗 白 93%）
  std::wstring label;  // 模型短名 / 口径名（Consolas 8.5px 白 66%）
};

// 形态绘制的每帧输入。geom 已由场景烘焙最终位置（tuck/让位/dx 已并入），
// 形态按 items[i].x/y 直读、按 scale 自行放大；e 用于收缩态降不透明度。
// 画刷与文本格式由 DockScene 持有（设备代际重建），形态只借用。
struct DrawContext {
  D3DContext* d3d = nullptr;
  const IMaterial* material = nullptr;       // 球底/描边委托（必有）
  const DockGeom* geom = nullptr;            // 最终位置几何（必有）
  const std::vector<DockItem>* items = nullptr;
  int mid = 0;                               // 中心项下标（accent 高亮）
  double e = 1.0;                            // 滑出进度（0=收缩，1=展开）
  std::string edge = "right";
  ID2D1SolidColorBrush* brush = nullptr;     // 共享画刷
  IDWriteTextFormat* valueFmt = nullptr;     // Consolas 13 半粗 居中
  IDWriteTextFormat* labelFmt = nullptr;     // Consolas 8.5 居中
  BackdropCapture* backdrop = nullptr;       // 背景捕获（传给材质取玻璃底）
  float backdropDX = 0, backdropDY = 0;      // 背景纹理→窗口坐标平移
};

class IForm {
public:
  virtual ~IForm() = default;
  virtual std::string id() const = 0;                    // "arc"
  virtual DockGeom layout(int n, int screenH, const std::string& edge) const = 0;
  virtual void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const = 0;
};

// 注册内建形态（render/forms/arc.cpp）
void registerArcForm(Registry<IForm>& reg);

} // namespace okmeter::render
