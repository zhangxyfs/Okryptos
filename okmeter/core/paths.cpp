#include "paths.h"
#include <cstdlib>

namespace okmeter {

std::filesystem::path kimiHome() {
  if (const wchar_t* p = _wgetenv(L"KIMI_CODE_HOME"); p && *p)
    return std::filesystem::path(p);
  if (const wchar_t* up = _wgetenv(L"USERPROFILE"); up && *up)
    return std::filesystem::path(up) / ".kimi-code";
  return std::filesystem::path(".kimi-code");
}

std::filesystem::path okmeterDir() {
  std::filesystem::path base;
  if (const wchar_t* up = _wgetenv(L"USERPROFILE"); up && *up) base = up;
  else base = ".";
  auto d = base / ".okryptos" / "okmeter";
  std::error_code ec;
  std::filesystem::create_directories(d, ec);
  return d;
}

std::string pathU8(const std::filesystem::path& p) {
  auto s = p.generic_u8string();
  return std::string(s.begin(), s.end());
}

} // namespace okmeter
