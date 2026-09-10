#include "paths.h"
#include <cstdlib>

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

namespace {

std::wstring envOrEmpty(const wchar_t* name) {
  const DWORD need = GetEnvironmentVariableW(name, nullptr, 0);
  if (need == 0) return L"";
  std::wstring buf(need - 1, L'\0');
  GetEnvironmentVariableW(name, buf.data(), need);
  return buf;
}

} // namespace

namespace okmeter {

std::filesystem::path kimiHome() {
  const std::wstring home = envOrEmpty(L"KIMI_CODE_HOME");
  if (!home.empty())
    return std::filesystem::path(home);
  const std::wstring up = envOrEmpty(L"USERPROFILE");
  if (!up.empty())
    return std::filesystem::path(up) / ".kimi-code";
  return std::filesystem::path(".kimi-code");
}

std::filesystem::path okmeterDir() {
  const std::wstring up = envOrEmpty(L"USERPROFILE");
  std::filesystem::path base = up.empty() ? std::filesystem::path(".")
                                          : std::filesystem::path(up);
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
