// render/d3d.h —— 透明渲染上下文：D3D11 → D2D1 → DWrite → 分层窗口上屏
#pragma once

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <wrl/client.h>
#include <d3d11.h>
#include <dxgi1_2.h>
#include <d2d1_1.h>
#include <dwrite.h>
#include <string>

namespace okmeter::render {

// 呈现层：WS_EX_LAYERED + UpdateLayeredWindow（逐像素预乘 alpha，DWM 最老的透明
// 路径；DComp 路线已在实机实证废弃——DComp + WDA_EXCLUDEFROMCAPTURE 组合被 DWM
// 合成成不透明黑底，见规格 §8）。GPU 渲染到离屏 RT 纹理 → CopyResource 到
// staging → 行拷入 DIB → UpdateLayeredWindow（窗口尺寸量级小，回读开销可接受；
// 按需渲染停摆期零开销）。
// init 可重入：设备丢失（DEVICE_REMOVED/RESET）时释放全部 COM 对象按序重建。
// 每帧：begin() → D2D 绘制 → end()（EndDraw + 回读 + UpdateLayeredWindow）。
class D3DContext {
public:
  bool init(HWND hwnd, int w, int h);   // 失败返回 false；可重入
  void resize(int w, int h);
  bool begin();                         // 备好帧目标位图并 BeginDraw
  bool end();                           // EndDraw + 回读上屏；设备丢失时重建
  ID2D1DeviceContext* dc() const { return dc_.Get(); }
  IDWriteFactory* dwrite() const { return dwrite_.Get(); }
  bool ok() const { return dc_ != nullptr; }
  // 设备代际：每次 init 重建成功 +1；资源缓存方应比较代际而非裸指针（防 ABA）
  unsigned generation() const { return generation_; }
  // 诊断：end() 内 init 重入次数与触发错误码/设备移除原因（捕获重启风暴取证）
  unsigned rebuilds() const { return rebuilds_; }
  HRESULT lastRebuildErr() const { return lastRebuildErr_; }
  HRESULT lastRemovedReason() const { return lastRemovedReason_; }
  // 共享 DXGI 设备（WGC 背景捕获互操作用；设备重建后代际变化，需重新获取）
  Microsoft::WRL::ComPtr<IDXGIDevice> dxgiDevice() const;

  // 诊断：把当前帧渲染目标存成 PNG（--shot 自检用；不受
  // SetWindowDisplayAffinity 自排除影响，产出应用真实绘制像素）
  bool saveFrame(const std::wstring& path) const;

private:
  void release();
  bool createFrameTargets();            // RT 纹理 + staging + DIB/memDC（随窗口尺寸）
  bool ensureTarget();                  // 从 RT 纹理建 ID2D1Bitmap1
  bool presentLayered();                // staging → DIB → UpdateLayeredWindow

  HWND hwnd_ = nullptr;
  int w_ = 0, h_ = 0;
  Microsoft::WRL::ComPtr<ID3D11Device> d3d_;
  Microsoft::WRL::ComPtr<ID2D1Factory1> d2df_;
  Microsoft::WRL::ComPtr<ID2D1Device> d2dDev_;
  Microsoft::WRL::ComPtr<ID2D1DeviceContext> dc_;
  Microsoft::WRL::ComPtr<IDWriteFactory> dwrite_;
  Microsoft::WRL::ComPtr<ID3D11Texture2D> rt_;       // GPU 渲染目标（B8G8R8A8）
  Microsoft::WRL::ComPtr<ID3D11Texture2D> staging_;  // CPU 回读（STAGING+READ）
  Microsoft::WRL::ComPtr<ID2D1Bitmap1> frame_;
  HDC memDC_ = nullptr;                 // UpdateLayeredWindow 源 DC（持有 DIB）
  HBITMAP dib_ = nullptr, dibOld_ = nullptr;
  void* dibBits_ = nullptr;             // DIB 像素（32bpp 预乘 BGRA，顶向下）
  unsigned generation_ = 0;
  unsigned rebuilds_ = 0;         // end() 内 init 重入次数（设备丢失重建）
  HRESULT lastRebuildErr_ = S_OK;     // 触发重建的 EndDraw/Present 错误码
  HRESULT lastRemovedReason_ = S_OK;  // GetDeviceRemovedReason（若可查询）
};

} // namespace okmeter::render
