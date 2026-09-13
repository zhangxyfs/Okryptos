// core/isotime.h —— ISO 8601 时间串 → epoch 毫秒（claude/codex/qwen 日志时间戳共用）
#pragma once
#include <cstdint>
#include <string>

namespace okmeter {

// 支持 "2026-08-12T10:44:13.573Z" 与 "±hh:mm" 偏移；解析失败返回 0
int64_t isoToMs(const std::string& iso);

// 本地墙钟（年月日时分秒毫秒）→ epoch 毫秒（mktime 本地时区）；失败返回 0
int64_t localToMs(int y, int mo, int d, int h, int mi, int s, int ms);

} // namespace okmeter
