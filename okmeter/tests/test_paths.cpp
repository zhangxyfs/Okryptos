#include "../core/paths.h"
#include "framework.h"
#include <cstdlib>

using namespace okmeter;

TEST(paths_kimi_home_env_priority) {
  _wputenv_s(L"KIMI_CODE_HOME", L"D:\\custom-kimi");
  CHECK(kimiHome() == std::filesystem::path(L"D:\\custom-kimi"));
  _wputenv_s(L"KIMI_CODE_HOME", L"");
  CHECK(kimiHome().filename() == ".kimi-code");
}

TEST(paths_okmeter_dir_autocreate) {
  _wputenv_s(L"USERPROFILE", L"");
  auto d = okmeterDir();  // USERPROFILE 缺失 → 回落 ./.okryptos/okmeter
  CHECK(d.filename() == "okmeter");
  CHECK(d.parent_path().filename() == ".okryptos");
  std::error_code ec;
  CHECK(std::filesystem::exists(d, ec));
  std::filesystem::remove_all(d.parent_path(), ec);  // 只删 ./.okryptos 整棵树
}

TEST(paths_path_u8_roundtrip) {
  CHECK(pathU8(std::filesystem::path("a") / "b.txt") == "a/b.txt");
}
