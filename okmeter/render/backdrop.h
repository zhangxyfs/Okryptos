// render/backdrop.h —— WGC 实时背景捕获：抓窗口所在显示器内容（排除自身窗口），
// 为材质玻璃化提供背景纹理（区域采样由 Task 3 消费，本类只暴露最新帧纹理）。
#pragma once

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <wrl/client.h>
#include <inspectable.h>
#include <d3d11.h>
#include <dxgi.h>

namespace okmeter::render {

// 线程契约：FrameArrived 回调运行在 WGC 线程池线程，只交换"最新帧纹理 + dirty
// 标志"（SRWLOCK 保护）并向 UI 线程 PostMessage 唤醒（kMsgDirty，线程安全），
// 严禁触碰 Aggregator/Store/Config 等 UI 线程对象；
// start/stop/retry 与 acquire/dirty/markClean 均由 UI 线程调用。
// 降级链：affinity 缺失 → degraded()=true；CreateForMonitor/CreateFreeThreaded/
// StartCapture 任一失败 → ok()=false，均记录 backdrop.log（调用点 + HRESULT）。
class BackdropCapture {
public:
  ~BackdropCapture();

  // 完整管线：SetWindowDisplayAffinity 排除自身 → CreateForMonitor(窗口所在屏) →
  // CreateFreeThreaded(BGRA8, 显示器尺寸) → StartCapture。可重入（内部先 stop）。
  bool start(HWND hwnd, IDXGIDevice* dxgi);
  void stop();
  void retry();  // 用 start 记录的 hwnd/dxgi 重启（会话解锁/显示变化时）

  bool ok() const { return ok_; }
  bool degraded() const { return degraded_; }  // affinity 缺失/失败 → 液态玻璃不可用

  // 捕获纹理原点：纹理像素 (0,0) 对应的虚拟屏幕坐标（显示器左上角），
  // 供渲染侧把窗口坐标换算成纹理坐标；捕获未启动时为 (0,0)
  void capOrigin(int& x, int& y) const;

  // 渲染侧取最新帧：成功返回 AddRef 的整屏纹理（BGRA8，捕获尺寸）。
  // 顺带在帧变化时刷新 luma_（8×8 网格点采样均摊亮度，亮背景自适应墨色用）
  bool acquire(ID3D11Texture2D** out);
  float luma() const { return luma_; }  // 0..1 相对亮度；无捕获/未采样为 0
  bool dirty() const;
  void markClean();
  unsigned long long frameCount() const;  // 累计到达帧数（验收/诊断）
  void note(const wchar_t* fmt, ...);  // 诊断：往 backdrop.log 追加一条（调用点自证）

  // 新背景帧到达时 PostMessage 唤醒 UI 线程的消息号（onMessage 包装器接手 syncFrames）
  static constexpr UINT kMsgDirty = WM_APP + 0x4B;

  // 内部入口（由 cpp 内 FrameHandler 转发，勿直接调用）：FrameArrived，WGC 线程池线程
  void onFrame(IUnknown* poolSender);

private:
  void log(const wchar_t* fmt, ...);
  bool failAt(const wchar_t* where, HRESULT hr);  // 记日志并返回 false

  HWND hwnd_ = nullptr;
  Microsoft::WRL::ComPtr<IDXGIDevice> dxgi_;

  mutable SRWLOCK lock_ = SRWLOCK_INIT;  // 保护 latest_/dirty_/frames_/capW_/capH_/device_
  Microsoft::WRL::ComPtr<ID3D11Texture2D> latest_;
  bool dirty_ = false;
  unsigned long long frames_ = 0;
  int capW_ = 0, capH_ = 0;  // 帧池尺寸（内容尺寸变化时回调线程 Recreate）
  int capX_ = 0, capY_ = 0;  // 捕获显示器左上角虚拟屏幕坐标（纹理原点）
  float luma_ = 0.0f;        // 最近帧均摊亮度（acquire 内网格点采样）
  bool lumaDirty_ = true;    // 新帧到达置位，acquire 采样后清
  Microsoft::WRL::ComPtr<ID3D11Texture2D> lumaStage_;  // 8×8 STAGING 采样缓冲

  bool ok_ = false;
  bool degraded_ = false;
  bool roInit_ = false;
  bool handlerAdded_ = false;
  long long frameTokenValue_ = 0;
  // ABI 对象以 IInspectable 持有（头文件不引 WinRT ABI 头；cpp 内按接口使用）
  Microsoft::WRL::ComPtr<IInspectable> item_;
  Microsoft::WRL::ComPtr<IInspectable> pool_;
  Microsoft::WRL::ComPtr<IInspectable> session_;
  Microsoft::WRL::ComPtr<IInspectable> device_;
};

} // namespace okmeter::render
