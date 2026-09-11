// ui/menu.h —— 自绘玻璃右键菜单：内容模型 + 文本测量布局 + D2D 绘制。
// 不用原生 Win32 菜单（用户亲测原生菜单不达标）：菜单就是 dock 窗口内的一块
// 绘制区（同 swapchain，窗口扩出菜单区，参照详情卡的卡区扩展做法），无模态泵，
// 动画循环照常运行。菜单底委托 material.drawCardBack（深色底 + hairline），
// 圆角 10、弹出动画 120ms（透明度 + 向屏缘方向 6px 滑入，原型 cardin 同款）。
#pragma once

#include "../render/d3d.h"
#include "../render/material.h"
#include <string>
#include <vector>

namespace okmeter {

// 菜单条目：Title 标题行（不可点）/ Item 映射项或动作项 / Separator 分隔线 /
// Disabled 灰化项（设置…，Task 7 点亮）
struct MenuEntry {
  enum Kind { Title, Item, Separator, Disabled } kind = Item;
  std::wstring label;
  std::string value;        // 映射项的 mapping 值（"auto"/"total:*"/"model:*"）
  bool tick = false;        // 当前映射值打 ✓
  int action = 0;           // 0=映射项，1=设置（灰化占位），2=换边，3=退出
  float y0 = 0, y1 = 0;     // 行区间（菜单内坐标，layout 填充）
};

// 画刷与文本格式是设备相关资源：比较 dc 指针 + 设备代际（DockScene::ensure 同款）
class GlassMenu {
public:
  bool open = false;
  int slot = -1;            // 球上右键的槽位；-1 = 弧线/空白菜单
  int hover = -1;           // 悬停条目下标（仅 Item）
  D2D1_RECT_F rect{};       // 窗口客户区坐标（app 定位）
  int width = 200, height = 0;

  std::vector<MenuEntry> entries;

  void layout(render::D3DContext& d3d);   // 文本测量 → width/height + 行区间
  void place(float x, float y);           // 定位（rect 左上）
  int hit(int x, int y) const;            // 命中可点条目下标（-1=菜单外/不可点行）
  bool contains(int x, int y) const;      // 点在菜单矩形内（含不可点行）
  void draw(render::D3DContext& d3d, render::IMaterial& material);

private:
  bool ensure(render::D3DContext& d3d);

  ID2D1DeviceContext* seen_ = nullptr;
  unsigned seenGen_ = 0;  // 上次重建资源时的设备代际（防 dc 地址复用 ABA 误判）
  Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brush_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> itemFmt_;    // Segoe UI 12.5（原型 .ctx button）
  Microsoft::WRL::ComPtr<IDWriteTextFormat> titleFmt_;   // Segoe UI 10（原型 .ctx h6）
};

} // namespace okmeter
