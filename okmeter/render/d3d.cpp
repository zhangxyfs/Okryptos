#include "d3d.h"

using Microsoft::WRL::ComPtr;

namespace okmeter::render {

void D3DContext::release() {
  frame_.Reset();
  swap_.Reset();
  visual_.Reset();
  target_.Reset();
  dcomp_.Reset();
  dwrite_.Reset();
  dc_.Reset();
  d2dDev_.Reset();
  d2df_.Reset();
  d3d_.Reset();
}

bool D3DContext::init(HWND hwnd, int w, int h) {
  release();
  hwnd_ = hwnd;
  w_ = w;
  h_ = h;

  // 1. D3D11 设备（BGRA 供 D2D 互操作；硬件失败回落 WARP）
  const D3D_FEATURE_LEVEL levels[] = { D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0 };
  const UINT flags = D3D11_CREATE_DEVICE_BGRA_SUPPORT;
  HRESULT hr = D3D11CreateDevice(nullptr, D3D_DRIVER_TYPE_HARDWARE, nullptr, flags,
                                 levels, 2, D3D11_SDK_VERSION, &d3d_, nullptr, nullptr);
  if (FAILED(hr)) {
    hr = D3D11CreateDevice(nullptr, D3D_DRIVER_TYPE_WARP, nullptr, flags,
                           levels, 2, D3D11_SDK_VERSION, &d3d_, nullptr, nullptr);
    if (FAILED(hr)) return false;
  }
  ComPtr<IDXGIDevice> dxgi;
  if (FAILED(d3d_.As(&dxgi))) return false;

  // 2. D2D1 factory → device → device context
  if (FAILED(D2D1CreateFactory(D2D1_FACTORY_TYPE_SINGLE_THREADED, d2df_.GetAddressOf()))) return false;
  if (FAILED(d2df_->CreateDevice(dxgi.Get(), &d2dDev_))) return false;
  if (FAILED(d2dDev_->CreateDeviceContext(D2D1_DEVICE_CONTEXT_OPTIONS_NONE, &dc_))) return false;

  // 3. DWrite factory（共享，设备无关）
  if (FAILED(DWriteCreateFactory(DWRITE_FACTORY_TYPE_SHARED, __uuidof(IDWriteFactory),
                                 reinterpret_cast<IUnknown**>(dwrite_.GetAddressOf())))) return false;

  // 4. DirectComposition：device → target(hwnd, 置顶) → visual → SetRoot
  if (FAILED(DCompositionCreateDevice(dxgi.Get(), IID_PPV_ARGS(&dcomp_)))) return false;
  if (FAILED(dcomp_->CreateTargetForHwnd(hwnd, TRUE, &target_))) return false;
  if (FAILED(dcomp_->CreateVisual(&visual_))) return false;
  if (FAILED(target_->SetRoot(visual_.Get()))) return false;

  // 5. 透明 swapchain 并挂到 visual
  return createSwapChain();
}

bool D3DContext::createSwapChain() {
  ComPtr<IDXGIDevice> dxgi;
  if (FAILED(d3d_.As(&dxgi))) return false;
  ComPtr<IDXGIAdapter> adapter;
  if (FAILED(dxgi->GetAdapter(&adapter))) return false;
  ComPtr<IDXGIFactory2> factory;
  if (FAILED(adapter->GetParent(IID_PPV_ARGS(&factory)))) return false;

  DXGI_SWAP_CHAIN_DESC1 desc{};
  desc.Width = (UINT)w_;
  desc.Height = (UINT)h_;
  desc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
  desc.SampleDesc.Count = 1;
  desc.BufferUsage = DXGI_USAGE_RENDER_TARGET_OUTPUT;
  desc.BufferCount = 2;
  desc.SwapEffect = DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL;
  desc.AlphaMode = DXGI_ALPHA_MODE_PREMULTIPLIED;
  if (FAILED(factory->CreateSwapChainForComposition(dxgi.Get(), &desc, nullptr, &swap_))) return false;
  if (FAILED(visual_->SetContent(swap_.Get()))) return false;
  return true;
}

bool D3DContext::ensureTarget() {
  if (frame_) return true;
  ComPtr<IDXGISurface> surface;
  if (FAILED(swap_->GetBuffer(0, IID_PPV_ARGS(&surface)))) return false;
  const D2D1_BITMAP_PROPERTIES1 props = D2D1::BitmapProperties1(
      D2D1_BITMAP_OPTIONS_TARGET | D2D1_BITMAP_OPTIONS_CANNOT_DRAW,
      D2D1::PixelFormat(DXGI_FORMAT_B8G8R8A8_UNORM, D2D1_ALPHA_MODE_PREMULTIPLIED));
  if (FAILED(dc_->CreateBitmapFromDxgiSurface(surface.Get(), &props, &frame_))) return false;
  dc_->SetTarget(frame_.Get());
  return true;
}

bool D3DContext::begin() {
  if (!ok() || !ensureTarget()) return false;
  dc_->BeginDraw();
  return true;
}

bool D3DContext::end() {
  HRESULT hr = dc_->EndDraw();
  if (hr == D2DERR_RECREATE_TARGET || hr == DXGI_ERROR_DEVICE_REMOVED || hr == DXGI_ERROR_DEVICE_RESET) {
    return init(hwnd_, w_, h_);  // 本帧丢弃，init 重入重建，下一帧重绘
  }
  if (FAILED(hr)) return false;
  hr = swap_->Present(1, 0);
  if (hr == DXGI_ERROR_DEVICE_REMOVED || hr == DXGI_ERROR_DEVICE_RESET) {
    return init(hwnd_, w_, h_);
  }
  if (FAILED(hr)) return false;
  return SUCCEEDED(dcomp_->Commit());
}

void D3DContext::resize(int w, int h) {
  if (w <= 0 || h <= 0 || (w == w_ && h == h_) || !swap_) return;
  w_ = w;
  h_ = h;
  dc_->SetTarget(nullptr);
  frame_.Reset();
  const HRESULT hr = swap_->ResizeBuffers(0, (UINT)w, (UINT)h, DXGI_FORMAT_UNKNOWN, 0);
  if (FAILED(hr)) (void)init(hwnd_, w_, h_);
  // 正常路径下帧位图在下一帧 begin() 的 ensureTarget 中重建
}

} // namespace okmeter::render
