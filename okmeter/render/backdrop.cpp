#include "backdrop.h"
#include "../core/paths.h"

#include <roapi.h>
#include <windows.graphics.directx.direct3d11.h>
#include <windows.graphics.directx.direct3d11.interop.h>
#include <windows.graphics.capture.h>
#include <Windows.Graphics.Capture.Interop.h>
#include <wrl.h>
#include <wrl/wrappers/corewrappers.h>
#include <cstdarg>
#include <cstdio>

namespace wgc = ABI::Windows::Graphics::Capture;
namespace wg = ABI::Windows::Graphics;
namespace wgd = ABI::Windows::Graphics::DirectX;
namespace wgd3d = ABI::Windows::Graphics::DirectX::Direct3D11;
namespace wf = ABI::Windows::Foundation;

using Microsoft::WRL::ComPtr;
using Microsoft::WRL::Wrappers::HStringReference;

namespace okmeter::render {
namespace {

// FrameArrived 处理器：FtmBase（agile）使 FreeThreaded 池可在线程池线程直调。
// 只转发给 BackdropCapture::onFrame；生命周期由池的引用持有，stop() 先
// remove_FrameArrived 再释放池，析构安全。
class FrameHandler final
    : public Microsoft::WRL::RuntimeClass<
          Microsoft::WRL::RuntimeClassFlags<Microsoft::WRL::ClassicCom>,
          wf::ITypedEventHandler<wgc::Direct3D11CaptureFramePool*, IInspectable*>,
          Microsoft::WRL::FtmBase> {
public:
  explicit FrameHandler(BackdropCapture* owner) : owner_(owner) {}
  // TSender 的 ABI 映射为默认接口 IDirect3D11CaptureFramePool*（GetAbiType）
  STDMETHOD(Invoke)(wgc::IDirect3D11CaptureFramePool* pool, IInspectable*) override {
    if (owner_) owner_->onFrame(pool);
    return S_OK;
  }

private:
  BackdropCapture* owner_;
};

} // namespace

BackdropCapture::~BackdropCapture() { stop(); }

void BackdropCapture::log(const wchar_t* fmt, ...) {
  wchar_t msg[512];
  va_list ap;
  va_start(ap, fmt);
  _vsnwprintf_s(msg, _TRUNCATE, fmt, ap);
  va_end(ap);
  SYSTEMTIME st{};
  GetLocalTime(&st);
  wchar_t line[640];
  _snwprintf_s(line, _TRUNCATE, L"[%02d:%02d:%02d.%03d] %s",
               st.wHour, st.wMinute, st.wSecond, st.wMilliseconds, msg);
  const std::string path = (okmeterDir() / "backdrop.log").string();
  FILE* fp = nullptr;
  if (fopen_s(&fp, path.c_str(), "a") == 0 && fp) {
    fwprintf(fp, L"%s\n", line);
    fclose(fp);
  }
  OutputDebugStringW(line);
  OutputDebugStringW(L"\n");
}

bool BackdropCapture::failAt(const wchar_t* where, HRESULT hr) {
  log(L"FAIL %s hr=0x%08lX → backdropOk=false", where, (unsigned long)hr);
  ok_ = false;
  return false;
}

bool BackdropCapture::start(HWND hwnd, IDXGIDevice* dxgi) {
  stop();
  degraded_ = false;
  hwnd_ = hwnd;
  dxgi_ = dxgi;
  if (!hwnd || !dxgi) {
    log(L"start: hwnd/dxgi 为空 → backdropOk=false");
    return false;
  }

  // 1. affinity 最先做：自身窗口从捕获中排除（API 缺失/失败 → 降级标记）
  using AffinityFn = BOOL(WINAPI*)(HWND, DWORD);
  const auto setAffinity = reinterpret_cast<AffinityFn>(
      GetProcAddress(GetModuleHandleW(L"user32.dll"), "SetWindowDisplayAffinity"));
  constexpr DWORD kWdaExclude = 0x00000011;  // WDA_EXCLUDEFROMCAPTURE（20H1+）
  constexpr DWORD kWdaMonitor = 0x00000001;  // WDA_MONITOR 兜底（自身窗口呈黑盒）
  if (!setAffinity) {
    degraded_ = true;
    log(L"SetWindowDisplayAffinity API 缺失 → 降级标记（液态玻璃不可用）");
  } else if (!setAffinity(hwnd, kWdaExclude)) {
    if (setAffinity(hwnd, kWdaMonitor)) {
      log(L"WDA_EXCLUDEFROMCAPTURE 不可用，回落 WDA_MONITOR");
    } else {
      degraded_ = true;
      log(L"SetWindowDisplayAffinity 失败 gle=%lu → 降级标记", GetLastError());
    }
  }

  // 2. WinRT 公寓（FreeThreaded 池回调需要 MTA）
  HRESULT hr = RoInitialize(RO_INIT_MULTITHREADED);
  if (hr == S_OK) {
    roInit_ = true;
  } else if (hr == RPC_E_CHANGED_MODE) {
    log(L"RoInitialize: 进程已是 STA，继续（hr=0x%08lX）", (unsigned long)hr);
  } else if (FAILED(hr) && hr != S_FALSE) {
    return failAt(L"RoInitialize", hr);
  }

  const HMONITOR mon = MonitorFromWindow(hwnd, MONITOR_DEFAULTTONEAREST);

  // 3. GraphicsCaptureItem：activation factory → IGraphicsCaptureItemInterop →
  //    CreateForMonitor（即 CreateFromMonitor 的桌面 ABI 入口）
  ComPtr<IGraphicsCaptureItemInterop> interop;
  hr = RoGetActivationFactory(
      HStringReference(RuntimeClass_Windows_Graphics_Capture_GraphicsCaptureItem).Get(),
      IID_PPV_ARGS(&interop));
  if (FAILED(hr)) return failAt(L"RoGetActivationFactory(GraphicsCaptureItem)", hr);

  ComPtr<wgc::IGraphicsCaptureItem> item;
  hr = interop->CreateForMonitor(mon, IID_PPV_ARGS(&item));
  if (FAILED(hr)) return failAt(L"CreateFromMonitor", hr);

  // 4. 渲染共享 DXGI 设备 → WinRT IDirect3DDevice
  ComPtr<IInspectable> inspDev;
  hr = CreateDirect3D11DeviceFromDXGIDevice(dxgi, &inspDev);
  if (FAILED(hr)) return failAt(L"CreateDirect3D11DeviceFromDXGIDevice", hr);
  ComPtr<wgd3d::IDirect3DDevice> dev;
  hr = inspDev.As(&dev);
  if (FAILED(hr) || !dev) return failAt(L"IDirect3DDevice QI", hr);

  // 5. FreeThreaded 帧池（BGRA8，尺寸=显示器）
  MONITORINFO mi{};
  mi.cbSize = sizeof(mi);
  (void)GetMonitorInfoW(mon, &mi);
  const wg::SizeInt32 size{ mi.rcMonitor.right - mi.rcMonitor.left,
                            mi.rcMonitor.bottom - mi.rcMonitor.top };
  if (size.Width <= 0 || size.Height <= 0) {
    log(L"显示器尺寸无效 → backdropOk=false");
    return false;
  }

  ComPtr<wgc::IDirect3D11CaptureFramePoolStatics2> poolStatics;
  hr = RoGetActivationFactory(
      HStringReference(RuntimeClass_Windows_Graphics_Capture_Direct3D11CaptureFramePool).Get(),
      IID_PPV_ARGS(&poolStatics));
  if (FAILED(hr)) return failAt(L"RoGetActivationFactory(Direct3D11CaptureFramePool)", hr);

  ComPtr<wgc::IDirect3D11CaptureFramePool> pool;
  hr = poolStatics->CreateFreeThreaded(
      dev.Get(), wgd::DirectXPixelFormat_B8G8R8A8UIntNormalized, 1, size, &pool);
  if (FAILED(hr)) return failAt(L"CreateFramePool(CreateFreeThreaded)", hr);

  // 6. FrameArrived（先注册回调再建会话，StartCapture 后才有帧流）
  auto handler = Microsoft::WRL::Make<FrameHandler>(this);
  if (!handler) {
    log(L"Make<FrameHandler> 失败 → backdropOk=false");
    return false;
  }
  EventRegistrationToken token{};
  hr = pool->add_FrameArrived(handler.Get(), &token);
  if (FAILED(hr)) return failAt(L"add_FrameArrived", hr);

  // 7. 会话 + StartCapture
  ComPtr<wgc::IGraphicsCaptureSession> session;
  hr = pool->CreateCaptureSession(item.Get(), &session);
  if (FAILED(hr)) {
    (void)pool->remove_FrameArrived(token);
    return failAt(L"CreateCaptureSession", hr);
  }
  hr = session->StartCapture();
  if (FAILED(hr)) {
    (void)pool->remove_FrameArrived(token);
    return failAt(L"StartCapture", hr);
  }

  AcquireSRWLockExclusive(&lock_);
  latest_.Reset();
  dirty_ = false;
  capW_ = size.Width;
  capH_ = size.Height;
  capX_ = mi.rcMonitor.left;
  capY_ = mi.rcMonitor.top;
  device_ = dev;
  ReleaseSRWLockExclusive(&lock_);
  item_ = item;
  pool_ = pool;
  session_ = session;
  frameTokenValue_ = token.value;
  handlerAdded_ = true;
  ok_ = true;
  log(L"capture started: monitor=%p %ldx%ld affinity=%s", (void*)mon,
      (long)size.Width, (long)size.Height, degraded_ ? L"degraded" : L"ok");
  return true;
}

void BackdropCapture::stop() {
  {
    // 局部引用须先于 RoUninitialize 释放（RoUninitialize 之后 Release WinRT
    // 对象会卡死/崩溃——退出路径实测死锁于此），内层作用域保证析构顺序
    ComPtr<wgc::IGraphicsCaptureSession> session;
    ComPtr<wgc::IDirect3D11CaptureFramePool> pool;
    if (session_) (void)session_.As(&session);
    if (pool_) (void)pool_.As(&pool);

    if (session) {
      ComPtr<wf::IClosable> c;
      if (SUCCEEDED(session.As(&c)) && c) (void)c->Close();
    }
    if (pool && handlerAdded_) {
      const EventRegistrationToken token{ frameTokenValue_ };
      (void)pool->remove_FrameArrived(token);
    }
    if (pool) {
      ComPtr<wf::IClosable> c;
      if (SUCCEEDED(pool.As(&c)) && c) (void)c->Close();
    }
  }
  session_.Reset();
  pool_.Reset();
  item_.Reset();
  handlerAdded_ = false;
  frameTokenValue_ = 0;

  AcquireSRWLockExclusive(&lock_);
  latest_.Reset();
  device_.Reset();
  dirty_ = false;
  ReleaseSRWLockExclusive(&lock_);

  ok_ = false;
  if (roInit_) {
    RoUninitialize();
    roInit_ = false;
  }
}

void BackdropCapture::retry() {
  if (!hwnd_ || !dxgi_) {
    log(L"retry: 无上下文（start 从未成功入参）");
    return;
  }
  log(L"retry: 重建捕获（会话解锁/显示变化）");
  (void)start(hwnd_, dxgi_.Get());
}

// WGC 线程池线程：只交换最新帧纹理 + dirty（SRWLOCK），严禁触碰 UI 线程对象。
void BackdropCapture::onFrame(IUnknown* poolSender) {
  if (!poolSender) return;
  ComPtr<wgc::IDirect3D11CaptureFramePool> pool;
  if (FAILED(poolSender->QueryInterface(IID_PPV_ARGS(&pool))) || !pool) return;
  ComPtr<wgc::IDirect3D11CaptureFrame> frame;
  if (FAILED(pool->TryGetNextFrame(&frame)) || !frame) return;

  wg::SizeInt32 size{};
  (void)frame->get_ContentSize(&size);

  ComPtr<ID3D11Texture2D> tex;
  ComPtr<wgd3d::IDirect3DSurface> surface;
  if (SUCCEEDED(frame->get_Surface(&surface)) && surface) {
    ComPtr<Windows::Graphics::DirectX::Direct3D11::IDirect3DDxgiInterfaceAccess> access;
    if (SUCCEEDED(surface->QueryInterface(IID_PPV_ARGS(&access))) && access)
      (void)access->GetInterface(IID_PPV_ARGS(&tex));
  }
  // 取完即归还帧（纹理生命周期由池管理，latest_ 的 ComPtr 保活）
  ComPtr<wf::IClosable> closable;
  if (SUCCEEDED(frame->QueryInterface(IID_PPV_ARGS(&closable))) && closable)
    (void)closable->Close();

  ComPtr<wgd3d::IDirect3DDevice> dev;
  bool resized = false;
  AcquireSRWLockExclusive(&lock_);
  if (tex) {
    latest_ = tex;
    dirty_ = true;
    ++frames_;
    if (size.Width != capW_ || size.Height != capH_) {
      capW_ = size.Width;
      capH_ = size.Height;
      resized = true;
      if (device_) (void)device_.As(&dev);
    }
  }
  ReleaseSRWLockExclusive(&lock_);
  // 内容尺寸变化（分辨率/DPI/拓扑）→ 回调线程 Recreate（FreeThreaded 池契约允许）
  if (resized && dev)
    (void)pool->Recreate(dev.Get(), wgd::DirectXPixelFormat_B8G8R8A8UIntNormalized, 1, size);
}

bool BackdropCapture::acquire(ID3D11Texture2D** out) {
  if (!out) return false;
  *out = nullptr;
  AcquireSRWLockShared(&lock_);
  if (latest_) (void)latest_.CopyTo(out);
  ReleaseSRWLockShared(&lock_);
  return *out != nullptr;
}

void BackdropCapture::capOrigin(int& x, int& y) const {
  AcquireSRWLockShared(&lock_);
  x = capX_;
  y = capY_;
  ReleaseSRWLockShared(&lock_);
}

bool BackdropCapture::dirty() const {
  AcquireSRWLockShared(&lock_);
  const bool d = dirty_;
  ReleaseSRWLockShared(&lock_);
  return d;
}

void BackdropCapture::markClean() {
  AcquireSRWLockExclusive(&lock_);
  dirty_ = false;
  ReleaseSRWLockExclusive(&lock_);
}

unsigned long long BackdropCapture::frameCount() const {
  AcquireSRWLockShared(&lock_);
  const unsigned long long n = frames_;
  ReleaseSRWLockShared(&lock_);
  return n;
}

} // namespace okmeter::render
