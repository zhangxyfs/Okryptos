// render/material.h —— 材质接口（IMaterial）：球底/卡底/弧线描边等皮肤行为。
// 材质不决定"项在哪"（那是 IForm 的事），只管"表面长什么样"。
#pragma once

#include "d3d.h"
#include "../core/registry.h"
#include "../ui/geometry.h"
#include <string>

namespace okmeter::render {

class D3DContext;
class BackdropCapture;

// 单球底绘制输入。center/r 为最终值（悬停放大已并入 r，勿再设变换）；
// backdropDX/DY 把背景纹理像素平移到窗口坐标（tex(0,0) 落在窗口此坐标）；
// dimmed 为收缩态不透明度乘子（原型 .dock:not(.open) .orb opacity .55）。
struct OrbStyleCtx {
  D3DContext* d3d = nullptr;              // 设备代际（材质缓存设备资源用）
  BackdropCapture* backdrop = nullptr;    // nullptr / !ok() / degraded() → 退化纯色底
  ID2D1SolidColorBrush* brush = nullptr;  // 共享画刷（染色/描边填充）
  D2D1_POINT_2F center{};
  float r = 0;
  bool isCenter = false;                  // 中心项：accent 描边 + 光晕环
  bool isHot = false;                     // 悬停项：accent 描边 + 外发光
  float dimmed = 1.0f;
  float backdropDX = 0, backdropDY = 0;
};

class IMaterial {
public:
  virtual ~IMaterial() = default;
  virtual std::string id() const = 0;                    // "dark"
  virtual void drawOrbBack(ID2D1DeviceContext*, const OrbStyleCtx&) const = 0;  // 球体底（玻璃/折射/光）
  virtual void drawCardBack(ID2D1DeviceContext*, const D2D1_RECT_F&, float radius) const = 0;
  virtual void drawArcStroke(ID2D1DeviceContext*, const DockGeom&) const = 0;
  virtual void onPointer(float x, float y) const = 0;    // 跟手光/镜面高光的光源位置
};

// 注册内建材质（render/materials/dark.cpp）
void registerDarkMaterial(Registry<IMaterial>& reg);

} // namespace okmeter::render
