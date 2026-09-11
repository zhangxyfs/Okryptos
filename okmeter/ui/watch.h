// ui/watch.h —— 会话目录变更监听（RDCW 异步 + manual-reset event）
#pragma once

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <filesystem>

namespace okmeter {

// ReadDirectoryChangesW 重叠监听：子树递归，事件触发只意味着"有变化"，
// 调用方据此即时执行一次与轮询同路径的 poll（监听建立后退避 2s 轮询为 30s 兜底——
// 缓冲溢出会静默丢事件；监听建立失败则维持 2s 纯轮询）。
// start 任一步失败返回 false → 调用方回落纯轮询。
class DirWatcher {
public:
  DirWatcher() = default;
  ~DirWatcher();
  DirWatcher(const DirWatcher&) = delete;
  DirWatcher& operator=(const DirWatcher&) = delete;

  bool start(const std::filesystem::path& dir);  // 建句柄 + 投递首请求；失败 false
  bool signaled();  // 事件已触发 → true 并重新投递；未监听返回 false
  HANDLE eventHandle() const { return event_; }  // 完成事件（消息循环可纳入等待集）

private:
  bool arm();  // 投递一次 ReadDirectoryChangesW（OVERLAPPED 绑 event_）

  HANDLE dir_ = INVALID_HANDLE_VALUE;
  HANDLE event_ = nullptr;  // manual-reset
  OVERLAPPED ov_{};
  alignas(8) unsigned char buf_[4096]{};  // 通知缓冲（内容不消费）
};

} // namespace okmeter
