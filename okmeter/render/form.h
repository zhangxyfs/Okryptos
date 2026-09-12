// render/form.h —— 形态接口（IForm）：布局 + 项绘制。形态只管"球/项在哪、项内画什么"，
// 球底/卡底/弧线描边等皮肤行为委托给 IMaterial（render/material.h）。
#pragma once

#include "d3d.h"
#include "../ui/geometry.h"
#include <memory>
#include <string>
#include <vector>

namespace okmeter::render {

class IMaterial;
class BackdropCapture;

// 省略号裁剪文本（原型 text-overflow:ellipsis；长模型名不换行、截断加省略号）
inline void drawTextTrimmed(D3DContext& d3d, ID2D1DeviceContext* dc,
                            ID2D1Brush* brush, const std::wstring& s,
                            IDWriteTextFormat* fmt, const D2D1_RECT_F& rc) {
  if (!dc || !brush || !fmt || s.empty()) return;
  Microsoft::WRL::ComPtr<IDWriteTextLayout> tl;
  if (FAILED(d3d.dwrite()->CreateTextLayout(s.c_str(), (UINT32)s.size(), fmt,
                                            rc.right - rc.left, rc.bottom - rc.top,
                                            &tl)))
    return;
  const DWRITE_TRIMMING trim{DWRITE_TRIMMING_GRANULARITY_CHARACTER, 0, 0};
  (void)tl->SetTrimming(&trim, nullptr);
  (void)tl->SetWordWrapping(DWRITE_WORD_WRAPPING_NO_WRAP);
  dc->DrawTextLayout(D2D1::Point2F(rc.left, rc.top), tl.Get(), brush);
}

struct DockItem {
  std::wstring value;  // 紧凑值（球内主文本，Consolas 13px 半粗 白 93%）
  std::wstring label;  // 模型短名 / 口径名（Consolas 8.5px 白 66%）
  double ratio = 0;    // 该项值/全部项最大值（胶囊底部占比条；clamp 4%~100%）
  double raw = 0;      // 原始数值（电平柱占比/nixie 数码/wave 历史的数值源）
  std::vector<double> hist;  // 最近 26 次 raw 采样（wave 波形流 sparkline）
};

// 形态绘制的每帧输入。geom 已由场景烘焙最终位置（tuck/让位/dx 已并入），
// 形态按 items[i].x/y 直读、按 scale 自行放大；e 用于收缩态降不透明度。
// 画刷与文本格式由 DockScene 持有（设备代际重建），形态只借用。
struct DrawContext;  // fwd for helper below

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
  IDWriteTextFormat* capNameFmt = nullptr;   // 胶囊左名：Segoe UI 10 左对齐
  IDWriteTextFormat* capValFmt = nullptr;    // 胶囊右值：Consolas 11 半粗 右对齐
  IDWriteTextFormat* hubFmt = nullptr;       // 罗盘中心值：Consolas 15 居中
  IDWriteTextFormat* satFmt = nullptr;       // 罗盘卫星值：Consolas 9.5 居中
  BackdropCapture* backdrop = nullptr;       // 背景捕获（传给材质取玻璃底）
  float backdropDX = 0, backdropDY = 0;      // 背景纹理→窗口坐标平移
};

class IForm {
public:
  virtual ~IForm() = default;
  virtual std::string id() const = 0;                    // "arc"
  virtual DockGeom layout(int n, int screenH, const std::string& edge) const = 0;
  virtual void drawItems(ID2D1DeviceContext* dc, const DrawContext& ctx) const = 0;
  // 动画帧推进（罗盘收缩态旋转用；e=弹簧滑出进度）。默认无持续动画
  virtual void tick(double dt, double e) { (void)dt; (void)e; }
  // 有进行中的形态动画（罗盘收缩态旋转）需持续重绘时返回 true（默认 false）
  virtual bool wantsTick() const { return false; }
  virtual double hoverScale() const { return 1.34; }  // 悬停放大（胶囊 1.08 / 罗盘 1.22）
  virtual double hoverPush() const { return 10; }     // 邻项让位 px（罗盘 0：环形不推挤）
  // 详情卡卡半径（原型 RADII 表：arc 30 / capsule 22 / 罗盘中心 43 卫星 23）
  virtual double cardRadius(int idx, int mid) const { (void)idx; (void)mid; return 30; }
};

// 各形态工厂（render/forms/*.cpp 实现；catalog.cpp 与 kFormCatalog 一一配对）
std::unique_ptr<IForm> createArcForm();
std::unique_ptr<IForm> createCapsuleForm();
std::unique_ptr<IForm> createCompassForm();
std::unique_ptr<IForm> createLevelForm();
std::unique_ptr<IForm> createWaveForm();
std::unique_ptr<IForm> createNixieForm();

} // namespace okmeter::render
