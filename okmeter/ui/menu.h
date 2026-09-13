// ui/menu.h —— 自绘玻璃右键菜单（三级级联）：内容模型 + 文本测量布局 + D2D 绘制。
// 不用原生 Win32 菜单（用户亲测原生菜单不达标）：菜单就是 dock 窗口内的一块
// 绘制区（同渲染目标，窗口扩出菜单区，参照详情卡的卡区扩展做法），无模态泵，
// 动画循环照常运行。菜单底委托 material.drawCardBack（深色底 + hairline），
// 圆角 10、弹出动画 120ms（透明度 + 向屏缘方向 6px 滑入，原型 cardin 同款）。
// 级联：主列（总量 ▸/模型 ▸/设置/换边/退出）→ 二级（总量四口径 / 厂商分组）
// → 三级（厂商下具体模型）；悬停父项即展开子列（Windows 菜单同款）。
#pragma once

#include "../render/d3d.h"
#include "../render/material.h"
#include <string>
#include <vector>

namespace okmeter {

// 菜单条目：Title 标题行（不可点）/ Item 映射项或动作项 / Separator 分隔线 /
// Disabled 灰化项 / Parent 父项（悬停展开子列，右端 ▸）
struct MenuEntry {
  enum Kind { Title, Item, Separator, Disabled, Parent } kind = Item;
  std::wstring label;
  std::string value;        // 映射项的 mapping 值（"total:*"/"model:*"）；厂商父项=厂商段名
  bool tick = false;        // 当前映射值打 ✓
  int action = 0;           // 0=映射项，1=设置，2=换边，3=退出
  int sub = 0;              // Parent 专用：1=总量 2=模型 3=厂商（value 为厂商段名）
  float y0 = 0, y1 = 0;     // 行区间（列内坐标，layout 填充）
};

// 一列菜单（主列/二级/三级各一列，结构相同）
struct MenuColumn {
  std::vector<MenuEntry> entries;
  D2D1_RECT_F rect{};       // 窗口客户区坐标
  int width = 0, height = 0;
  int hover = -1;           // 悬停条目下标（仅 Item/Parent）

  void clear() { entries.clear(); rect = {}; width = height = 0; hover = -1; }
};

// 画刷与文本格式是设备相关资源：比较 dc 指针 + 设备代际（DockScene::ensure 同款）
class GlassMenu {
public:
  bool open = false;
  int slot = -1;            // 球上右键的槽位；-1 = 弧线/空白菜单
  int hover = -1;           // 主列悬停条目下标（仅 Item/Parent）
  D2D1_RECT_F rect{};       // 主列矩形（窗口客户区坐标，app 定位）
  int width = 200, height = 0;

  std::vector<MenuEntry> entries;  // 主列条目

  // 级联子列（sub1 未开时 entries 为空）
  MenuColumn sub1, sub2;
  int parent1 = -1;         // 主列中展开 sub1 的条目下标（-1=未展开）
  int parent2 = -1;         // sub1 中展开 sub2 的条目下标
  int subDir = 1;           // 子列展开方向：+1 向右（父项画 ▸ 于行尾）-1 向左（◂ 于行首）

  void layout(render::D3DContext& d3d);   // 文本测量 → 各列 width/height + 行区间
  void place(float x, float y);           // 主列定位（rect 左上）
  void placeSub(MenuColumn& col, float x, float y);  // 子列定位
  // 命中可点条目：返回值 = 列号*1000 + 条目下标（-1=菜单外/不可点行）
  int hit(int x, int y) const;
  bool contains(int x, int y) const;      // 点在任一列矩形内（外点判定用）
  D2D1_RECT_F bounds() const;             // 全列并集（窗口扩窗用）
  void draw(render::D3DContext& d3d, render::IMaterial& material);

private:
  bool ensure(render::D3DContext& d3d);
  void layoutCol(render::D3DContext& d3d, MenuColumn& col);
  void drawCol(render::D3DContext& d3d, render::IMaterial& material, MenuColumn& col,
               int hoverIdx);

  ID2D1DeviceContext* seen_ = nullptr;
  unsigned seenGen_ = 0;  // 上次重建资源时的设备代际（防 dc 地址复用 ABA 误判）
  Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brush_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> itemFmt_;    // Segoe UI 12.5（原型 .ctx button）
  Microsoft::WRL::ComPtr<IDWriteTextFormat> titleFmt_;   // Segoe UI 10（原型 .ctx h6）
};

} // namespace okmeter
