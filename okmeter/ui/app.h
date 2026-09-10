// ui/app.h —— Dock 窗口应用：透明无边框置顶窗口 + 三 timer 数据接线 + 两态弹簧
#pragma once

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "../core/binding.h"
#include "../core/config.h"
#include "../core/spring.h"
#include "../render/d3d.h"
#include "../render/dock_scene.h"
#include "geometry.h"
#include "watch.h"
#include <cstdint>
#include <memory>
#include <vector>

namespace okmeter {

class Store;
class KimiAdapter;

// 单线程契约：poll 与渲染读都在 UI 线程，由 WM_TIMER 串行驱动（aggregator.h 约定）。
// 三 timer：动画帧 16ms（弹簧 step + 需要时重绘 + SetWindowPos + 每 500ms 检查
// RDCW 目录监听 → 触发则提前 poll）、数据轮询 2000ms（poll→flush→重建
// bindings/文本缓存→重绘）、相对时间刷新 30s。
// 右键菜单：换边（edge 互换 + saveConfig 持久化 + 重建几何）、退出（flush 游标）。
// 两态：emerge 弹簧 e∈[0,1]，右缘窗口 x = 屏宽 - lerp(24, g.w, e) - (wide?268:0)；
// 展开态窗口宽 g.w+268（卡区在球区屏内侧，右缘靠左），收缩态宽 g.w；
// 鼠标进入 e→1，离开 600ms 迟滞后 e→0。
class DockApp {
public:
  ~DockApp();
  int run(HINSTANCE inst);  // DPI → 注册类 → 建窗 → 数据/渲染初始化 → 消息循环

private:
  static LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp);
  LRESULT onMessage(UINT msg, WPARAM wp, LPARAM lp);

  void render();          // layoutArc → applyHover → dock_scene 一帧（含详情卡）
  void pollData();        // kimi.poll→agg.add→flush→rebuildItems→重绘
  void rebuildItems();    // resolveBindings + 文本缓存（值/短名）+ 详情卡重组
  void rebuildCard();     // 按 hoverIdx 组装详情卡（hover 变化/数据刷新时调用）
  void rebuildLayout();   // 屏高/边 → 窗口矩形 + SetWindowPos
  void updatePosition();  // 按弹簧 e 移动窗口 x（SetWindowPos）
  void setEmergeTarget(double t);
  void setWide(bool w);   // 展开态窗口宽 g.w+268（卡区）；收缩态回 g.w
  void flipEdge();        // 换边：edge 互换 → saveConfig → 重建位置几何
  void exitApp();         // 退出：flush 游标落盘 → DestroyWindow

  render::D3DContext d3d_;
  render::DockScene scene_;
  HWND hwnd_ = nullptr;
  DirWatcher watch_;          // sessions 目录 RDCW 监听（start 失败则纯轮询）

  std::unique_ptr<Store> store_;
  std::unique_ptr<KimiAdapter> kimi_;
  Config cfg_;
  std::vector<Binding> bindings_;
  std::vector<render::DockItem> items_;

  Spring emerge_;
  double emergeTarget_ = 0;
  bool emerged_ = true;       // 弹簧已静止在 target（避免静止帧空转）
  int hoverIdx_ = -1;
  bool trackingLeave_ = false;
  bool wide_ = false;         // 窗口含 268px 卡区（展开态）
  render::DetailCard card_;   // 悬停详情卡缓存（rebuildCard 重组）
  int winY_ = 0;              // 垂直居中 y（rebuildLayout 重算）
  int winH_ = 0;              // 窗口高（卡垂直夹取/宽度切换用）
  int dockW_ = 150;           // layoutArc g.w（位置插值用）
  int64_t lastWatchMs_ = 0;   // 上次 RDCW 检查时刻（动画帧里每 500ms 一次）
};

} // namespace okmeter
