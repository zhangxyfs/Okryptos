// core/atomic.h —— 原子文本写：tmp + MoveFileExW(REPLACE_EXISTING)
#pragma once
#include <filesystem>
#include <string>

namespace okmeter {

bool atomicWriteText(const std::filesystem::path& file, const std::string& content);

} // namespace okmeter
