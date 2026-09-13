// adapters/tail.h —— jsonl 游标增量读取（新适配器共用；游标只推进到最后一个完整行，
// 结尾半行留给下一轮；文件截断/轮换自动从头读）
#pragma once
#include "../core/paths.h"
#include "../core/store.h"
#include <filesystem>
#include <fstream>
#include <functional>
#include <string>
#include <string_view>

namespace okmeter {

inline bool tailJsonl(const std::filesystem::path& f, Store* store,
                      const std::function<void(std::string_view)>& onLine) {
  const std::string key = pathU8(f);
  int64_t off = store->cursor(key);
  std::error_code ec;
  const auto size = (int64_t)std::filesystem::file_size(f, ec);
  if (ec || size == off) return false;
  if (size < off) off = 0;

  std::ifstream in(f, std::ios::binary);
  if (!in) return false;
  in.seekg(off);
  std::string chunk((std::istreambuf_iterator<char>(in)), std::istreambuf_iterator<char>());

  const size_t lastNl = chunk.rfind('\n');
  if (lastNl == std::string::npos) return false;
  const std::string_view work(chunk.data(), lastNl + 1);

  size_t pos = 0;
  while (pos < work.size()) {
    const size_t nl = work.find('\n', pos);
    onLine(work.substr(pos, nl - pos));
    pos = nl + 1;
  }
  store->setCursor(key, off + (int64_t)work.size());
  return true;
}

} // namespace okmeter
