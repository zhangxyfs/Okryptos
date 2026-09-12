// render/material.h —— 材质接口（IMaterial）：球底/卡底/弧线描边等皮肤行为。
// 材质不决定"项在哪"（那是 IForm 的事），只管"表面长什么样"。
#pragma once

#include "d3d.h"
#include "../ui/geometry.h"
#include <memory>
#include <string>

namespace okmeter::render {

class D3DContext;
class BackdropCapture;

// 亮背景自适应墨色：背景亮（luma>0.55）用深墨，暗背景用浅墨（修"白底白字不可见"）
inline D2D1_COLOR_F inkOn(float luma, float a) {
  return luma > 0.55f ? D2D1::ColorF(0.09f, 0.11f, 0.14f, a)
                      : D2D1::ColorF(0.93f, 0.94f, 0.96f, a);
}

// 单球底绘制输入。center/r 为最终值（悬停放大已并入 r，勿再设变换）；
// backdropDX/DY 把背景纹理像素平移到窗口坐标（tex(0,0) 落在窗口此坐标）；
// dimmed 为收缩态不透明度乘子（原型 .dock:not(.open) .orb opacity .55）。
struct OrbStyleCtx {
  D3DContext* d3d = nullptr;              // 设备代际（材质缓存设备资源用）
  BackdropCapture* backdrop = nullptr;    // nullptr / !ok() / degraded() → 退化纯色底
  ID2D1SolidColorBrush* brush = nullptr;  // 共享画刷（染色/描边填充）
  D2D1_POINT_2F center{};
  float r = 0;
  float halfW = 0;                        // >r 时项为胶囊（圆角矩形 半宽 halfW 半高 r）；否则圆
  bool isCenter = false;                  // 中心项：accent 描边 + 光晕环
  bool isHot = false;                     // 悬停项：accent 描边 + 外发光
  float dimmed = 1.0f;
  float backdropDX = 0, backdropDY = 0;
  float cornerR = 0;                      // 圆角矩形角半径（0=与 r 相同=胶囊；level/wave/nixie 10~12）
};

class IMaterial {
public:
  virtual ~IMaterial() = default;
  virtual std::string id() const = 0;                    // "dark"
  virtual float backdropLuma() const { return 0.0f; }  // 最近背景帧亮度（亮背景自适应墨色：>0.55 用深墨）
  virtual void drawOrbBack(ID2D1DeviceContext*, const OrbStyleCtx&) const = 0;  // 球体底（玻璃/折射/光）
  virtual void drawCardBack(ID2D1DeviceContext*, const D2D1_RECT_F&, float radius) const = 0;
  // 项间连线/底层光效；g.connector=false（胶囊/罗盘）时不画项间折线
  //（glow 材质的环境光/粒子层不受 connector 影响，照常绘制）
  virtual void drawArcStroke(ID2D1DeviceContext*, const DockGeom&,
                             const std::string& edge) const = 0;  // edge="left"/"right"
  virtual void onPointer(float x, float y) const = 0;    // 跟手光/镜面高光的光源位置
  virtual void onPointerLeave() const {}                 // 指针离开（光感熄灭/粒子消散）
  virtual void onPress(float, float) const {}            // 按下触点（按压光晕）
  virtual void onPulse() const {}                        // 新数据到达（粒子迸散触发）
  // 有进行中的光效动画（粒子/光晕/柔光过渡）需持续重绘时返回 true；
  // app 在弹簧静止后仍按动画帧率调 render（默认 false：纯事件驱动）
  virtual bool wantsTick() const { return false; }
};

// 各材质工厂（render/materials/*.cpp 实现；catalog.cpp 与 kMaterialCatalog 一一配对）
std::unique_ptr<IMaterial> createDarkMaterial();
std::unique_ptr<IMaterial> createFrostMaterial();
std::unique_ptr<IMaterial> createLiquidMaterial();
std::unique_ptr<IMaterial> createGlowMaterial();

} // namespace okmeter::render
