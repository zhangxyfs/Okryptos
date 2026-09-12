#include "d3d.h"
#include <wincodec.h>

using Microsoft::WRL::ComPtr;

namespace okmeter::render {

void D3DContext::release() {
  frame_.Reset();
  staging_.Reset();
  rt_.Reset();
  dc_.Reset();
  d2dDev_.Reset();
  d2df_.Reset();
  dwrite_.Reset();
  d3d_.Reset();
  if (memDC_) {
    if (dibOld_) SelectObject(memDC_, dibOld_);
    DeleteDC(memDC_);
    memDC_ = nullptr;
    dibOld_ = nullptr;
  }
  if (dib_) {
    DeleteObject(dib_);
    dib_ = nullptr;
    dibBits_ = nullptr;
  }
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

  // 4. 帧目标（RT 纹理 + staging + DIB）
  if (!createFrameTargets()) return false;
  ++generation_;  // 重建成功：代际 +1（资源缓存方据此失效）
  return true;
}

bool D3DContext::createFrameTargets() {
  if (w_ <= 0 || h_ <= 0) return false;
  // GPU 渲染目标纹理
  D3D11_TEXTURE2D_DESC rd{};
  rd.Width = (UINT)w_;
  rd.Height = (UINT)h_;
  rd.MipLevels = 1;
  rd.ArraySize = 1;
  rd.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
  rd.SampleDesc.Count = 1;
  rd.Usage = D3D11_USAGE_DEFAULT;
  rd.BindFlags = D3D11_BIND_RENDER_TARGET;
  if (FAILED(d3d_->CreateTexture2D(&rd, nullptr, &rt_))) return false;
  // CPU 回读 staging
  D3D11_TEXTURE2D_DESC sd = rd;
  sd.Usage = D3D11_USAGE_STAGING;
  sd.BindFlags = 0;
  sd.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
  if (FAILED(d3d_->CreateTexture2D(&sd, nullptr, &staging_))) return false;
  // 分层窗口像素源：32bpp 顶向下 DIB 挂 memDC
  if (!memDC_) {
    memDC_ = CreateCompatibleDC(nullptr);
    if (!memDC_) return false;
  } else if (dib_) {
    SelectObject(memDC_, dibOld_);
    DeleteObject(dib_);
    dib_ = nullptr;
  }
  BITMAPINFO bi{};
  bi.bmiHeader.biSize = sizeof(bi.bmiHeader);
  bi.bmiHeader.biWidth = w_;
  bi.bmiHeader.biHeight = -h_;  // 顶向下（与纹理行序一致）
  bi.bmiHeader.biPlanes = 1;
  bi.bmiHeader.biBitCount = 32;
  bi.bmiHeader.biCompression = BI_RGB;
  dib_ = CreateDIBSection(memDC_, &bi, DIB_RGB_COLORS, &dibBits_, nullptr, 0);
  if (!dib_ || !dibBits_) return false;
  dibOld_ = (HBITMAP)SelectObject(memDC_, dib_);
  return true;
}

bool D3DContext::ensureTarget() {
  if (frame_) return true;
  if (!rt_) return false;
  ComPtr<IDXGISurface> surface;
  if (FAILED(rt_.As(&surface))) return false;
  const D2D1_BITMAP_PROPERTIES1 props = D2D1::BitmapProperties1(
      D2D1_BITMAP_OPTIONS_TARGET | D2D1_BITMAP_OPTIONS_CANNOT_DRAW,
      D2D1::PixelFormat(DXGI_FORMAT_B8G8R8A8_UNORM, D2D1_ALPHA_MODE_PREMULTIPLIED));
  if (FAILED(dc_->CreateBitmapFromDxgiSurface(surface.Get(), &props, &frame_))) return false;
  dc_->SetTarget(frame_.Get());
  return true;
}

Microsoft::WRL::ComPtr<IDXGIDevice> D3DContext::dxgiDevice() const {
  Microsoft::WRL::ComPtr<IDXGIDevice> dxgi;
  if (d3d_) (void)d3d_.As(&dxgi);
  return dxgi;
}

// RT 纹理 → staging → WIC PNG（预乘 BGRA 转非预乘编码）
bool D3DContext::saveFrame(const std::wstring& path) const {
  if (!rt_ || !d3d_) return false;
  ComPtr<ID3D11DeviceContext> imm;
  d3d_->GetImmediateContext(&imm);
  if (!imm) return false;
  imm->CopyResource(staging_.Get(), rt_.Get());
  D3D11_TEXTURE2D_DESC desc{};
  rt_->GetDesc(&desc);
  (void)CoInitializeEx(nullptr, COINIT_MULTITHREADED);  // 已初始化则 S_FALSE
  ComPtr<IWICImagingFactory> wic;
  if (FAILED(CoCreateInstance(CLSID_WICImagingFactory, nullptr,
                              CLSCTX_INPROC_SERVER, IID_PPV_ARGS(&wic))))
    return false;
  D3D11_MAPPED_SUBRESOURCE m{};
  if (FAILED(imm->Map(staging_.Get(), 0, D3D11_MAP_READ, 0, &m))) return false;
  bool ok = false;
  ComPtr<IWICBitmap> bmp;
  if (SUCCEEDED(wic->CreateBitmapFromMemory(
          desc.Width, desc.Height, GUID_WICPixelFormat32bppPBGRA, m.RowPitch,
          m.RowPitch * desc.Height, static_cast<BYTE*>(m.pData), &bmp))) {
    ComPtr<IWICFormatConverter> conv;
    if (SUCCEEDED(wic->CreateFormatConverter(&conv)) &&
        SUCCEEDED(conv->Initialize(bmp.Get(), GUID_WICPixelFormat32bppBGRA,
                                   WICBitmapDitherTypeNone, nullptr, 0.0,
                                   WICBitmapPaletteTypeCustom))) {
      ComPtr<IWICStream> stream;
      ComPtr<IWICBitmapEncoder> enc;
      ComPtr<IWICBitmapFrameEncode> frame;
      if (SUCCEEDED(wic->CreateStream(&stream)) &&
          SUCCEEDED(stream->InitializeFromFilename(path.c_str(), GENERIC_WRITE)) &&
          SUCCEEDED(wic->CreateEncoder(GUID_ContainerFormatPng, nullptr, &enc)) &&
          SUCCEEDED(enc->Initialize(stream.Get(), WICBitmapEncoderNoCache)) &&
          SUCCEEDED(enc->CreateNewFrame(&frame, nullptr)) &&
          SUCCEEDED(frame->Initialize(nullptr)) &&
          SUCCEEDED(frame->SetSize(desc.Width, desc.Height)) &&
          SUCCEEDED(frame->SetResolution(96.0, 96.0))) {
        WICPixelFormatGUID fmt = GUID_WICPixelFormat32bppBGRA;
        if (SUCCEEDED(frame->SetPixelFormat(&fmt)) &&
            SUCCEEDED(frame->WriteSource(conv.Get(), nullptr)) &&
            SUCCEEDED(frame->Commit()) && SUCCEEDED(enc->Commit()))
          ok = true;
      }
    }
  }
  imm->Unmap(staging_.Get(), 0);
  return ok;
}

bool D3DContext::begin() {
  if (!ok() || !ensureTarget()) return false;
  dc_->BeginDraw();
  return true;
}

// staging → DIB（行拷贝，处理 RowPitch 对齐）→ UpdateLayeredWindow 上屏
bool D3DContext::presentLayered() {
  if (!staging_ || !dibBits_ || !memDC_) return false;
  ComPtr<ID3D11DeviceContext> imm;
  d3d_->GetImmediateContext(&imm);
  if (!imm) return false;
  imm->CopyResource(staging_.Get(), rt_.Get());
  D3D11_MAPPED_SUBRESOURCE m{};
  if (FAILED(imm->Map(staging_.Get(), 0, D3D11_MAP_READ, 0, &m))) return false;
  const int rowBytes = w_ * 4;
  auto* dst = static_cast<BYTE*>(dibBits_);
  const auto* src = static_cast<const BYTE*>(m.pData);
  for (int y = 0; y < h_; ++y)
    memcpy(dst + (size_t)y * rowBytes, src + (size_t)y * m.RowPitch, (size_t)rowBytes);
  imm->Unmap(staging_.Get(), 0);
  SIZE sz{ w_, h_ };
  POINT srcPt{ 0, 0 };
  BLENDFUNCTION bf{ AC_SRC_OVER, 0, 255, AC_SRC_ALPHA };
  // nullptr dst/位置：只更新内容不动窗口（位置由 SetWindowPos 管理）
  return UpdateLayeredWindow(hwnd_, nullptr, nullptr, &sz, memDC_, &srcPt, 0, &bf,
                             ULW_ALPHA) != FALSE;
}

bool D3DContext::end() {
  HRESULT hr = dc_->EndDraw();
  if (hr == D2DERR_RECREATE_TARGET || hr == DXGI_ERROR_DEVICE_REMOVED || hr == DXGI_ERROR_DEVICE_RESET) {
    ++rebuilds_;
    lastRebuildErr_ = hr;
    if (d3d_) lastRemovedReason_ = d3d_->GetDeviceRemovedReason();
    (void)init(hwnd_, w_, h_);  // 本帧丢弃，重建设备；返回 false 让调用方补画补呈
    return false;  // 帧丢弃必须显式 false（调用方 render() 有界重试）
  }
  if (FAILED(hr)) return false;
  if (!presentLayered()) return false;
  return true;
}

void D3DContext::resize(int w, int h) {
  if (w <= 0 || h <= 0 || (w == w_ && h == h_) || !rt_) return;
  w_ = w;
  h_ = h;
  dc_->SetTarget(nullptr);
  frame_.Reset();
  (void)createFrameTargets();  // 失败则下一帧 begin() 的 ensureTarget 兜不住 → end 重建
}

} // namespace okmeter::render
