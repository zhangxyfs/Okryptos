// ui/app.h —— Dock 窗口应用：透明无边框置顶窗口 + D3D/DComp 渲染骨架
#pragma once

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "../render/d3d.h"

namespace okmeter {

// 本任务骨架：右缘 150px 宽透明窗口，渲染测试图案（accent 绿圆 + Consolas 白文本）；
// 数据接线在 Task 7。
class DockApp {
public:
  int run(HINSTANCE inst);  // DPI → 注册类 → 建窗 → init 渲染 → 消息循环；返回退出码

private:
  static LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp);
  void render();            // 一帧测试图案：清透明 → 绿圆 → 白文本 → Present/Commit

  render::D3DContext d3d_;
  HWND hwnd_ = nullptr;
};

} // namespace okmeter
