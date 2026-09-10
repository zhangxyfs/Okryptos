// ui/watch.cpp —— 会话目录变更监听：RDCW 异步 + manual-reset event
#include "watch.h"

namespace okmeter {

DirWatcher::~DirWatcher() {
  if (dir_ != INVALID_HANDLE_VALUE) {
    CancelIoEx(dir_, &ov_);
    DWORD bytes = 0;
    GetOverlappedResult(dir_, &ov_, &bytes, TRUE);  // 等 IRP 落定再关句柄（取消落定返回 FALSE 属正常）
    CloseHandle(dir_);
  }
  if (event_) CloseHandle(event_);
}

bool DirWatcher::arm() {
  ov_ = OVERLAPPED{};
  ov_.hEvent = event_;
  DWORD bytes = 0;
  return ReadDirectoryChangesW(dir_, buf_, (DWORD)sizeof(buf_), TRUE,
             FILE_NOTIFY_CHANGE_LAST_WRITE | FILE_NOTIFY_CHANGE_FILE_NAME |
             FILE_NOTIFY_CHANGE_SIZE,
             &bytes, &ov_, nullptr) != 0;
}

bool DirWatcher::start(const std::filesystem::path& dir) {
  dir_ = CreateFileW(dir.c_str(), FILE_LIST_DIRECTORY,
                     FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
                     nullptr, OPEN_EXISTING,
                     FILE_FLAG_BACKUP_SEMANTICS | FILE_FLAG_OVERLAPPED, nullptr);
  if (dir_ == INVALID_HANDLE_VALUE) return false;
  event_ = CreateEventW(nullptr, TRUE, FALSE, nullptr);  // manual-reset
  if (!event_ || !arm()) {
    if (event_) { CloseHandle(event_); event_ = nullptr; }
    CloseHandle(dir_);
    dir_ = INVALID_HANDLE_VALUE;
    return false;
  }
  return true;
}

bool DirWatcher::signaled() {
  if (!event_) return false;
  if (WaitForSingleObject(event_, 0) != WAIT_OBJECT_0) return false;
  DWORD bytes = 0;
  GetOverlappedResult(dir_, &ov_, &bytes, FALSE);  // 确认完成、回收重叠结构
  ResetEvent(event_);
  return arm();  // 重新投递；失败则监听静默失效（轮询仍兜底）
}

} // namespace okmeter
