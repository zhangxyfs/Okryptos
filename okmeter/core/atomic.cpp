#include "atomic.h"
#include <fstream>

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

namespace okmeter {

bool atomicWriteText(const std::filesystem::path& file, const std::string& content) {
  std::error_code ec;
  std::filesystem::create_directories(file.parent_path(), ec);
  auto tmp = file;
  tmp += L".tmp";
  {
    std::ofstream out(tmp, std::ios::binary | std::ios::trunc);
    if (!out) {
      fwprintf(stderr, L"atomicWriteText: open tmp failed %s gle=%lu\n",
               file.c_str(), GetLastError());
      return false;
    }
    out << content;
    out.flush();
    if (!out) return false;
  }
  if (!MoveFileExW(tmp.c_str(), file.c_str(),
                   MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)) {
    fwprintf(stderr, L"atomicWriteText: MoveFileExW failed %s gle=%lu\n",
             file.c_str(), GetLastError());
    std::filesystem::remove(tmp, ec);
    return false;
  }
  return true;
}

} // namespace okmeter
