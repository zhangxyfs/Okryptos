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

namespace {

// env 优先，否则 USERPROFILE/<fallback>
std::filesystem::path homeOf(const wchar_t* env, const wchar_t* fallback) {
  const std::wstring e = envOrEmpty(env);
  if (!e.empty()) return std::filesystem::path(e);
  const std::wstring up = envOrEmpty(L"USERPROFILE");
  if (!up.empty()) return std::filesystem::path(up) / fallback;
  return std::filesystem::path(fallback);
}

} // namespace

std::filesystem::path claudeHome() { return homeOf(L"CLAUDE_CONFIG_HOME", L".claude"); }

std::filesystem::path codexHome() { return homeOf(L"CODEX_HOME", L".codex"); }

std::filesystem::path qwenHome() { return homeOf(L"QWEN_HOME", L".qwen"); }

std::filesystem::path zcodeHome() { return homeOf(L"OK_ZCODE_HOME", L".zcode"); }

std::filesystem::path workbuddyHome() { return homeOf(L"WORKBUDDY_HOME", L".workbuddy"); }

std::filesystem::path reasonixHome() {
  for (const wchar_t* env : { L"OK_REASONIX_HOME", L"REASONIX_HOME" }) {
    const std::wstring e = envOrEmpty(env);
    if (!e.empty()) return std::filesystem::path(e);
  }
  const std::wstring ad = envOrEmpty(L"APPDATA");
  if (!ad.empty()) return std::filesystem::path(ad) / "reasonix";
  return homeOf(L"", L".reasonix");
}

std::filesystem::path hanakoHome() { return homeOf(L"HANAKO_HOME", L".hanako"); }

std::filesystem::path okmeterDir() {
  const std::wstring up = envOrEmpty(L"USERPROFILE");
  std::filesystem::path base = up.empty() ? std::filesystem::path(".")
                                          : std::filesystem::path(up);
  auto d = base / ".okryptos" / "okmeter";
  std::error_code ec;
  std::filesystem::create_directories(d, ec);
  return d;
}

std::filesystem::path userProfile() {
  const std::wstring up = envOrEmpty(L"USERPROFILE");
  return up.empty() ? std::filesystem::path(".") : std::filesystem::path(up);
}

std::string pathU8(const std::filesystem::path& p) {
  auto s = p.generic_u8string();
  return std::string(s.begin(), s.end());
}

} // namespace okmeter
