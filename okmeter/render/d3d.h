// render/d3d.h —— 透明合成渲染上下文：D3D11 → D2D1 → DWrite → DirectComposition
#pragma once

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <wrl/client.h>
#include <d3d11.h>
#include <dxgi1_2.h>
#include <d2d1_1.h>
#include <dwrite.h>
#include <dcomp.h>
#include <string>

namespace okmeter::render {

// 透明 swapchain（B8G8R8A8 + PREMULTIPLIED + FLIP_SEQUENTIAL）经 DComp visual 上屏。
// init 可重入：设备丢失（DEVICE_REMOVED/RESET）时释放全部 COM 对象按序重建。
// 每帧：begin() → D2D 绘制 → end()（EndDraw + Present(0,0) 非阻塞 + Commit；DWM 按 vsync 合成）。
class D3DContext {
public:
  bool init(HWND hwnd, int w, int h);   // 失败返回 false；可重入
  void resize(int w, int h);
  bool begin();                         // 备好帧目标位图并 BeginDraw
  bool end();                           // EndDraw + Present + Commit；设备丢失时重建
  ID2D1DeviceContext* dc() const { return dc_.Get(); }
  IDWriteFactory* dwrite() const { return dwrite_.Get(); }
  bool ok() const { return dc_ != nullptr; }
  // 设备代际：每次 init 重建成功 +1；资源缓存方应比较代际而非裸指针（防 ABA）
  unsigned generation() const { return generation_; }
  // 共享 DXGI 设备（WGC 背景捕获互操作用；设备重建后代际变化，需重新获取）
  Microsoft::WRL::ComPtr<IDXGIDevice> dxgiDevice() const;

  // 诊断：把当前 swapchain back buffer 存成 PNG（--shot 自检用；不受
  // SetWindowDisplayAffinity 自排除影响，产出应用真实绘制像素）
  bool saveFrame(const std::wstring& path) const;

private:
  void release();
  bool createSwapChain();
  bool ensureTarget();                  // 从 swapchain back buffer 建 ID2D1Bitmap1

  HWND hwnd_ = nullptr;
  int w_ = 0, h_ = 0;
  Microsoft::WRL::ComPtr<ID3D11Device> d3d_;
  Microsoft::WRL::ComPtr<ID2D1Factory1> d2df_;
  Microsoft::WRL::ComPtr<ID2D1Device> d2dDev_;
  Microsoft::WRL::ComPtr<ID2D1DeviceContext> dc_;
  Microsoft::WRL::ComPtr<IDWriteFactory> dwrite_;
  Microsoft::WRL::ComPtr<IDCompositionDevice> dcomp_;
  Microsoft::WRL::ComPtr<IDCompositionTarget> target_;
  Microsoft::WRL::ComPtr<IDCompositionVisual> visual_;
  Microsoft::WRL::ComPtr<IDXGISwapChain1> swap_;
  Microsoft::WRL::ComPtr<ID2D1Bitmap1> frame_;
  unsigned generation_ = 0;
};

} // namespace okmeter::render
