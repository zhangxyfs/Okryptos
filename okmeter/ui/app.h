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
#include <memory>
#include <vector>

namespace okmeter {

class Store;
class KimiAdapter;

// 单线程契约：poll 与渲染读都在 UI 线程，由 WM_TIMER 串行驱动（aggregator.h 约定）。
// 三 timer：动画帧 16ms（弹簧 step + 需要时重绘 + SetWindowPos）、
// 数据轮询 2000ms（poll→flush→重建 bindings/文本缓存→重绘）、相对时间刷新 30s。
// 两态：emerge 弹簧 e∈[0,1]，右缘窗口 x = 屏宽 - lerp(24, g.w, e)；
// 鼠标进入 e→1，离开 600ms 迟滞后 e→0。
class DockApp {
public:
  ~DockApp();
  int run(HINSTANCE inst);  // DPI → 注册类 → 建窗 → 数据/渲染初始化 → 消息循环

private:
  static LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp);
  LRESULT onMessage(UINT msg, WPARAM wp, LPARAM lp);

  void render();          // layoutArc → applyHover → dock_scene 一帧
  void pollData();        // kimi.poll→agg.add→flush→rebuildItems→重绘
  void rebuildItems();    // resolveBindings + 文本缓存（值/短名）
  void rebuildLayout();   // 屏高/边 → 窗口矩形 + SetWindowPos
  void updatePosition();  // 按弹簧 e 移动窗口 x（SetWindowPos）
  void setEmergeTarget(double t);

  render::D3DContext d3d_;
  render::DockScene scene_;
  HWND hwnd_ = nullptr;

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
  int winY_ = 0;              // 垂直居中 y（rebuildLayout 重算）
  int dockW_ = 150;           // layoutArc g.w（位置插值用）
};

} // namespace okmeter
