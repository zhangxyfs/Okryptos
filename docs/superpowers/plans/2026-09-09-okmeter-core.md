# OkMeter 核心框架 + Kimi 采集适配器 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 OkMeter（Windows 桌面 token 监视器）实现可扩展的三层骨架中的"核心框架 + 采集适配器"两层：统一 usage 事件模型、聚合统计、游标持久化、模块注册表、Kimi Code 适配器（wire.jsonl 扫描/增量读/容错），全部表驱动单测覆盖，外加一个命令行冒烟 exe。

**Architecture:** 纯 C++20 静态库式代码组织（core/ + adapters/kimi/），无 UI、无第三方依赖；测试用自带微型断言框架（tests/framework.h 静态注册 + CHECK 宏）。本计划是后续渲染层（Plan 2：DComp 窗口 + D3D11 材质/形态）、设置背板（Plan 3）、托盘与打包（Plan 4）的地基。

**Tech Stack:** MSVC（VS Build Tools，vswhere 定位）+ Windows SDK ≥ 10.0.20348，C++20，/MT 静态 CRT，零外部库。

**Spec 源:** `docs/2026-09-08-token-dock-design.md` §2/§3.6/§3.7/§5/§6。视觉相关章节（§3.2~§3.5）不在本计划范围。

## Global Constraints

- 平台仅 Windows；本计划代码不得包含任何 UI/窗口调用（窗口在 Plan 2）。
- 零第三方依赖：JSON 解析用自写 `core/minjson.*`，不引 nlohmann/jsoncpp。
- 统一 usage 事件字段（逐字）：`model / agentId / sessionId / inputOther / inputCacheRead / inputCacheCreation / output / timeMs / usageScope`。
- home 解析：`KIMI_CODE_HOME` 环境变量优先，否则 `~/.kimi-code`。
- 持久化：`~/.okryptos/okmeter/state.json`，原子写（写 `.tmp` 后 `MoveFileExW(MOVEFILE_REPLACE_EXISTING)`）。
- 解析容错：坏行跳过、未知 `type` 跳过、缺 `usage` 字段跳过；游标只推进到最后一个完整行。
- 只读 `~/.kimi-code`，不写不删；不开网络端口、无 IPC。
- 编码：源码 UTF-8（cl 加 `/utf-8`），路径处理一律 UTF-8 字符串 ↔ `std::filesystem::path`。
- 测试命令固定为 `okmeter\build.bat`（编译单测 → 跑单测 → 编译 OkMeter.exe，任一失败非零退出）。
- 中文注释/输出允许；commit message 格式 `feat(okmeter): 中文描述`。

## 文件结构

```
okmeter/
  build.bat               # vswhere 定位 vcvars64 → 编译+跑单测 → 编译 OkMeter.exe
  version.rc              # exe 版本资源（0.1.0 占位，sync-version 接线属 Plan 4）
  core/
    event.h               # UsageEvent 统一事件 + Sums 之外的纯数据结构
    minjson.h/.cpp        # 极简 JSON 解析/序列化（容错）
    aggregator.h/.cpp     # Aggregator：all/today/week/session × 全局/模型 聚合
    store.h/.cpp          # Store：游标 + 聚合快照，state.json 原子写
    paths.h/.cpp          # kimiHome() / okmeterDir() / pathU8()
    registry.h            # Registry<T>：模块注册表（适配器/渲染模块通用）
  adapters/
    adapter.h             # IAdapter 接口 + EventSink
    kimi/adapter.h/.cpp   # KimiAdapter：sessions/**/wire.jsonl 游标增量扫描
  tests/
    framework.h           # TEST/CHECK/CHECK_EQ 微型断言 + 静态注册
    main.cpp              # 测试 runner
    test_sanity.cpp       # 骨架自检
    test_minjson.cpp
    test_paths.cpp
    test_aggregator.cpp
    test_store.cpp
    test_kimi_adapter.cpp
    test_registry.cpp     # 伪造适配器契约测试
  app/
    main.cpp              # 冒烟入口：扫一次真实 home，打印各口径总量
```

接口约定（后续任务依赖的精确签名）：

- `okmeter::UsageEvent`（core/event.h）：字段见 Global Constraints；`int64_t total() const`。
- `okmeter::Sums`（core/aggregator.h）：`inputOther/inputCacheRead/inputCacheCreation/output` 四个 int64 + `total()` + `plus(const Sums&)`。
- `okmeter::Aggregator`：`add(const UsageEvent&)`、`Sums all()/today(int64_t nowMs)/week(int64_t nowMs)/session()`、`Sums modelToday/modelWeek/modelSession(const std::string& id, [int64_t nowMs])`、`std::vector<std::string> modelsByRecency()`、`const ModelStat* model(const std::string&)`、`json::Value toJson() const`、`bool fromJson(const json::Value&)`、静态 `int dayKey(int64_t ms)/weekStartKey(int64_t ms)`。
- `okmeter::Store`：`Store(std::filesystem::path dir)`、`int64_t cursor(const std::string&)`、`void setCursor(const std::string&, int64_t)`、`Aggregator& agg()`、`bool load()`、`bool flush()`。
- `okmeter::paths`：`std::filesystem::path kimiHome()`、`std::filesystem::path okmeterDir()`、`std::string pathU8(const std::filesystem::path&)`。
- `okmeter::IAdapter`（adapters/adapter.h）：`virtual std::string id() const = 0`、`virtual int poll(const EventSink& sink) = 0`；`using EventSink = std::function<void(const UsageEvent&)>`。
- `okmeter::KimiAdapter`：`KimiAdapter(std::filesystem::path kimiHome, Store* store)`。
- `okmeter::Registry<T>`（core/registry.h）：`void add(const std::string&, Factory)`、`std::unique_ptr<T> create(const std::string&) const`、`std::vector<std::string> names() const`。
- `okmeter::json`：`Value`（variant 容器，`.obj()/.num(dflt)/.str()/.find(key)/.isObject()`）、`bool parse(const std::string&, Value&)`、`std::string dump(const Value&)`、`num(int64_t)/str(const std::string&)` 构造。

---

### Task 1: 工程骨架与构建脚本

**Files:**
- Create: `okmeter/build.bat`
- Create: `okmeter/version.rc`
- Create: `okmeter/tests/framework.h`
- Create: `okmeter/tests/main.cpp`
- Create: `okmeter/tests/test_sanity.cpp`
- Create: `okmeter/app/main.cpp`
- Modify: `.gitignore`（追加 `okmeter/build/`）

**Interfaces:**
- Consumes: 无
- Produces: 测试框架宏 `TEST(name)` / `CHECK(cond)` / `CHECK_EQ(a,b)`（tests/framework.h）；构建命令 `okmeter\build.bat`。

- [ ] **Step 1: 写测试框架与自检**

`okmeter/tests/framework.h`：

```cpp
// tests/framework.h —— 微型断言框架：静态注册 + 失败计数，零依赖
#pragma once
#include <cstdio>
#include <vector>

namespace okmeter::test {

struct Case { const char* name; void (*fn)(); };

inline std::vector<Case>& cases() { static std::vector<Case> c; return c; }
inline int& failures() { static int f = 0; return f; }

struct Auto {
  Auto(const char* n, void (*f)()) { cases().push_back({n, f}); }
};

} // namespace okmeter::test

#define TEST(name) \
  static void name(); \
  static ::okmeter::test::Auto auto_##name(#name, name); \
  static void name()

#define CHECK(cond) do { if (!(cond)) { \
  std::printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond); \
  ++::okmeter::test::failures(); } } while (0)

#define CHECK_EQ(a, b) do { auto _va = (a); auto _vb = (b); if (!(_va == _vb)) { \
  std::printf("FAIL %s:%d: %s == %s\n", __FILE__, __LINE__, #a, #b); \
  ++::okmeter::test::failures(); } } while (0)
```

`okmeter/tests/main.cpp`：

```cpp
// tests/main.cpp —— 单测 runner：逐条跑静态注册的用例
#include "framework.h"

int main() {
  for (auto& c : okmeter::test::cases()) {
    int before = okmeter::test::failures();
    c.fn();
    std::printf("%s %s\n", okmeter::test::failures() == before ? "ok  " : "FAIL", c.name);
  }
  std::printf("%zu cases, %d failures\n",
              okmeter::test::cases().size(), okmeter::test::failures());
  return okmeter::test::failures() ? 1 : 0;
}
```

`okmeter/tests/test_sanity.cpp`：

```cpp
#include "framework.h"

TEST(sanity) {
  CHECK(1 + 1 == 2);
}
```

`okmeter/app/main.cpp`（占位冒烟，Task 8 换成真扫描）：

```cpp
#include <cstdio>

int main() {
  std::printf("OkMeter scaffold\n");
  return 0;
}
```

`okmeter/version.rc`：

```rc
#include <windows.h>

1 VERSIONINFO
FILEVERSION 0,1,0,0
PRODUCTVERSION 0,1,0,0
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "080404b0"
    BEGIN
      VALUE "FileDescription", "OkMeter Token Monitor\0"
      VALUE "ProductName", "Okryptos OkMeter\0"
      VALUE "FileVersion", "0.1.0.0\0"
      VALUE "ProductVersion", "0.1.0.0\0"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x804, 1200
  END
END
```

`okmeter/build.bat`：

```bat
@echo off
setlocal EnableExtensions
pushd "%~dp0"

rem ── 定位 MSVC（vswhere → vcvars64）──
set "VSW=%ProgramFiles(x86)%\Microsoft Visual Studio\Installer\vswhere.exe"
if not exist "%VSW%" set "VSW=%ProgramFiles%\Microsoft Visual Studio\Installer\vswhere.exe"
if not exist "%VSW%" (echo 错误：未找到 vswhere，请安装 VS Build Tools & popd & exit /b 1)
set "VS="
for /f "usebackq delims=" %%i in (`"%VSW%" -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`) do set "VS=%%i"
if not defined VS (echo 错误：未找到 MSVC C++ 工具集 & popd & exit /b 1)
call "%VS%\VC\Auxiliary\Build\vcvars64.bat" >nul || (echo 错误：vcvars64 初始化失败 & popd & exit /b 1)

if not exist build mkdir build
set "FLAGS=/nologo /std:c++20 /EHsc /W4 /utf-8 /O2 /MT /I."

echo [1/3] 编译单测...
cl %FLAGS% tests\*.cpp core\*.cpp adapters\kimi\*.cpp /Fo:build\ /Fe:build\okmeter-tests.exe || (popd & exit /b 1)

echo [2/3] 运行单测...
build\okmeter-tests.exe || (popd & exit /b 1)

echo [3/3] 编译 OkMeter.exe...
rc /nologo /fo build\version.res version.rc || (popd & exit /b 1)
cl %FLAGS% app\main.cpp core\*.cpp adapters\kimi\*.cpp build\version.res /Fo:build\ /Fe:build\OkMeter.exe || (popd & exit /b 1)

echo 完成：build\OkMeter.exe
popd
```

注意：cl 通配符 `core\*.cpp` 在目录为空时原样传给编译器会报"找不到文件"，属预期——后续任务填入 core 文件后消失；Task 1 为保证 build.bat 全绿，先创建两个空占位：`okmeter/core/.gitkeep` 不行，必须让通配符有匹配。处理方式：Task 1 先建 `okmeter/core/placeholder.cpp` 与 `okmeter/adapters/kimi/placeholder.cpp`，内容均为一行注释：

```cpp
// placeholder：让 build.bat 的通配符非空，后续任务替换
namespace okmeter {}
```

`.gitignore` 追加一行：

```
okmeter/build/
```

- [ ] **Step 2: 跑构建**

Run: `okmeter\build.bat`
Expected: `[1/3] 编译单测...` 编译通过 → `[2/3]` 输出 `ok   sanity` 与 `1 cases, 0 failures` → `[3/3]` 产出 `build\OkMeter.exe`；最后打印 `完成：build\OkMeter.exe`。

- [ ] **Step 3: Commit**

```bash
git add okmeter .gitignore
git commit -m "feat(okmeter): C++ 工程骨架与 MSVC 构建脚本"
```

---

### Task 2: 极简 JSON（core/minjson）

**Files:**
- Create: `okmeter/core/minjson.h`
- Create: `okmeter/core/minjson.cpp`
- Test: `okmeter/tests/test_minjson.cpp`

**Interfaces:**
- Consumes: tests/framework.h（Task 1）
- Produces: `okmeter::json::{Value, Object, Array, parse, dump, num, str}`——Task 4/5/6 全部依赖。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_minjson.cpp`：

```cpp
#include "../core/minjson.h"
#include "framework.h"

TEST(json_parse_usage_record_line) {
  json::Value v;
  CHECK(json::parse(
    R"({"type":"usage.record","agentId":"main","model":"kimi-code/k3-256k","usage":{"inputOther":6243,"output":141,"inputCacheRead":18432,"inputCacheCreation":0},"usageScope":"turn","time":1788865482021})",
    v));
  CHECK(v.isObject());
  CHECK(v.find("type")->str() == "usage.record");
  const json::Value* u = v.find("usage");
  CHECK(u && u->isObject());
  CHECK_EQ((int64_t)u->find("inputCacheRead")->num(), 18432);
  CHECK_EQ((int64_t)v.find("time")->num(), 1788865482021LL);
  CHECK(v.find("missing") == nullptr);
}

TEST(json_rejects_bad_line) {
  json::Value v;
  CHECK(!json::parse("{not json", v));
  CHECK(!json::parse("", v));
  CHECK(!json::parse(R"({"a":1} trailing)", v));
}

TEST(json_unicode_escape) {
  json::Value v;
  CHECK(json::parse(R"({"s":"A中😀"})", v));
  CHECK(v.find("s")->str() == "A中😀");
}

TEST(json_dump_roundtrip) {
  json::Value v;
  json::Object o;
  o["n"] = json::num(42);
  o["s"] = json::str("a\"b\nc");
  json::Object inner;
  inner["x"] = json::num(7);
  o["o"].v = std::move(inner);
  v.v = std::move(o);
  std::string d = json::dump(v);
  json::Value back;
  CHECK(json::parse(d, back));
  CHECK_EQ((int64_t)back.find("n")->num(), 42);
  CHECK(back.find("s")->str() == "a\"b\nc");
  CHECK_EQ((int64_t)back.find("o")->find("x")->num(), 7);
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `okmeter\build.bat`
Expected: 编译错误 `cannot open include file: '../core/minjson.h'`（红）。

- [ ] **Step 3: 实现 minjson**

`okmeter/core/minjson.h`：

```cpp
// core/minjson.h —— 极简 JSON：仅支撑 wire.jsonl 行解析与 state.json 读写，零依赖
#pragma once
#include <cstdint>
#include <map>
#include <string>
#include <variant>
#include <vector>

namespace okmeter::json {

struct Value;
using Object = std::map<std::string, Value>;
using Array = std::vector<Value>;

struct Value {
  std::variant<std::nullptr_t, bool, double, std::string, Array, Object> v{nullptr};

  bool isObject() const { return std::holds_alternative<Object>(v); }

  const Object& obj() const {
    static const Object kEmpty;
    return isObject() ? std::get<Object>(v) : kEmpty;
  }
  double num(double dflt = 0) const {
    return std::holds_alternative<double>(v) ? std::get<double>(v) : dflt;
  }
  const std::string& str() const {
    static const std::string kEmpty;
    return std::holds_alternative<std::string>(v) ? std::get<std::string>(v) : kEmpty;
  }
  // 对象查找；非对象或缺键返回 nullptr
  const Value* find(const std::string& key) const {
    if (!isObject()) return nullptr;
    const Object& o = std::get<Object>(v);
    auto it = o.find(key);
    return it == o.end() ? nullptr : &it->second;
  }
};

bool parse(const std::string& text, Value& out);  // 失败返回 false（调用方跳过该行）
std::string dump(const Value& v);                 // 序列化（state.json 写盘用）

inline Value num(int64_t n) { Value v; v.v = (double)n; return v; }
inline Value str(const std::string& s) { Value v; v.v = s; return v; }

} // namespace okmeter::json
```

`okmeter/core/minjson.cpp`：

```cpp
#include "minjson.h"
#include <charconv>
#include <cmath>
#include <cstdio>
#include <cstring>

namespace okmeter::json {
namespace {

struct Parser {
  const char* p;
  const char* end;

  void ws() { while (p < end && (*p == ' ' || *p == '\t' || *p == '\r' || *p == '\n')) ++p; }

  bool keyword(const char* w) {
    size_t n = std::strlen(w);
    if ((size_t)(end - p) < n || std::memcmp(p, w, n) != 0) return false;
    p += n;
    return true;
  }

  bool value(Value& out) {
    ws();
    if (p >= end) return false;
    switch (*p) {
      case '{': return object(out);
      case '[': return array(out);
      case '"': { std::string s; if (!string(s)) return false; out.v = std::move(s); return true; }
      case 't': if (!keyword("true")) return false; out.v = true; return true;
      case 'f': if (!keyword("false")) return false; out.v = false; return true;
      case 'n': if (!keyword("null")) return false; out.v = nullptr; return true;
      default: return number(out);
    }
  }

  bool object(Value& out) {
    ++p;
    Object o;
    ws();
    if (p < end && *p == '}') { ++p; out.v = std::move(o); return true; }
    for (;;) {
      ws();
      if (p >= end || *p != '"') return false;
      std::string k;
      if (!string(k)) return false;
      ws();
      if (p >= end || *p != ':') return false;
      ++p;
      if (!value(o[k])) return false;
      ws();
      if (p >= end) return false;
      if (*p == ',') { ++p; continue; }
      if (*p == '}') { ++p; out.v = std::move(o); return true; }
      return false;
    }
  }

  bool array(Value& out) {
    ++p;
    Array a;
    ws();
    if (p < end && *p == ']') { ++p; out.v = std::move(a); return true; }
    for (;;) {
      Value el;
      if (!value(el)) return false;
      a.push_back(std::move(el));
      ws();
      if (p >= end) return false;
      if (*p == ',') { ++p; continue; }
      if (*p == ']') { ++p; out.v = std::move(a); return true; }
      return false;
    }
  }

  bool string(std::string& out) {
    ++p;  // 跳过开引号
    while (p < end) {
      char c = *p;
      if (c == '"') { ++p; return true; }
      if (c == '\\') {
        ++p;
        if (p >= end) return false;
        switch (*p) {
          case '"': out += '"'; break;
          case '\\': out += '\\'; break;
          case '/': out += '/'; break;
          case 'b': out += '\b'; break;
          case 'f': out += '\f'; break;
          case 'n': out += '\n'; break;
          case 'r': out += '\r'; break;
          case 't': out += '\t'; break;
          case 'u': if (!unicode(out)) return false; break;
          default: return false;
        }
        ++p;
        continue;
      }
      out += c;
      ++p;
    }
    return false;
  }

  int hex4(const char* s) {
    if (end - s < 4) return -1;
    int v = 0;
    for (int k = 0; k < 4; ++k) {
      char c = s[k];
      v <<= 4;
      if (c >= '0' && c <= '9') v |= c - '0';
      else if (c >= 'a' && c <= 'f') v |= c - 'a' + 10;
      else if (c >= 'A' && c <= 'F') v |= c - 'A' + 10;
      else return -1;
    }
    return v;
  }

  static void utf8(std::string& out, unsigned cp) {
    if (cp < 0x80) out += (char)cp;
    else if (cp < 0x800) { out += (char)(0xC0 | (cp >> 6)); out += (char)(0x80 | (cp & 63)); }
    else if (cp < 0x10000) {
      out += (char)(0xE0 | (cp >> 12));
      out += (char)(0x80 | ((cp >> 6) & 63));
      out += (char)(0x80 | (cp & 63));
    } else {
      out += (char)(0xF0 | (cp >> 18));
      out += (char)(0x80 | ((cp >> 12) & 63));
      out += (char)(0x80 | ((cp >> 6) & 63));
      out += (char)(0x80 | (cp & 63));
    }
  }

  // 进入时 p 指向 'u'；离开时 p 停在已消费的最后一个字符上（外层 ++p 收尾）
  bool unicode(std::string& out) {
    if (end - p < 5) return false;
    int cp = hex4(p + 1);
    if (cp < 0) return false;
    p += 4;
    if (cp >= 0xD800 && cp <= 0xDBFF && end - p >= 7 && p[1] == '\\' && p[2] == 'u') {
      int lo = hex4(p + 3);
      if (lo >= 0xDC00 && lo <= 0xDFFF) {
        cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
        p += 6;
      }
    }
    utf8(out, (unsigned)cp);
    return true;
  }

  bool number(Value& out) {
    const char* start = p;
    if (p < end && *p == '-') ++p;
    bool any = false;
    while (p < end && (isdigit((unsigned char)*p) || *p == '.' || *p == 'e' ||
                       *p == 'E' || *p == '+' || *p == '-')) {
      ++p;
      any = true;
    }
    if (!any) return false;
    double d = 0;
    auto r = std::from_chars(start, p, d);
    if (r.ec != std::errc()) return false;
    out.v = d;
    return true;
  }
};

void dumpInto(const Value& v, std::string& out) {
  if (std::holds_alternative<std::nullptr_t>(v.v)) { out += "null"; return; }
  if (const auto* b = std::get_if<bool>(&v.v)) { out += *b ? "true" : "false"; return; }
  if (const auto* d = std::get_if<double>(&v.v)) {
    char buf[40];
    double iv;
    if (std::modf(*d, &iv) == 0.0 && std::fabs(*d) < 9e15)
      std::snprintf(buf, sizeof buf, "%lld", (long long)iv);
    else
      std::snprintf(buf, sizeof buf, "%.17g", *d);
    out += buf;
    return;
  }
  if (const auto* s = std::get_if<std::string>(&v.v)) {
    out += '"';
    for (char c : *s) {
      switch (c) {
        case '"': out += "\\\""; break;
        case '\\': out += "\\\\"; break;
        case '\n': out += "\\n"; break;
        case '\r': out += "\\r"; break;
        case '\t': out += "\\t"; break;
        default:
          if ((unsigned char)c < 0x20) {
            char b[8];
            std::snprintf(b, sizeof b, "\\u%04x", (unsigned char)c);
            out += b;
          } else {
            out += c;
          }
      }
    }
    out += '"';
    return;
  }
  if (const auto* a = std::get_if<Array>(&v.v)) {
    out += '[';
    for (size_t i = 0; i < a->size(); ++i) {
      if (i) out += ',';
      dumpInto((*a)[i], out);
    }
    out += ']';
    return;
  }
  const Object& o = std::get<Object>(v.v);
  out += '{';
  bool first = true;
  for (const auto& [k, val] : o) {
    if (!first) out += ',';
    first = false;
    Value key;
    key.v = k;
    dumpInto(key, out);
    out += ':';
    dumpInto(val, out);
  }
  out += '}';
}

} // namespace

bool parse(const std::string& text, Value& out) {
  Parser pr{text.data(), text.data() + text.size()};
  if (!pr.value(out)) return false;
  pr.ws();
  return pr.p == pr.end;
}

std::string dump(const Value& v) {
  std::string s;
  dumpInto(v, s);
  return s;
}

} // namespace okmeter::json
```

同时删除占位文件：`okmeter/core/placeholder.cpp`（minjson.cpp 已让 `core\*.cpp` 非空）。

- [ ] **Step 4: 跑测试确认通过**

Run: `okmeter\build.bat`
Expected: 四个 json_* 用例全 `ok`，总计 `5 cases, 0 failures`。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 极简 JSON 解析与序列化（容错，零依赖）"
```

---

### Task 3: 路径解析（core/paths）

**Files:**
- Create: `okmeter/core/paths.h`
- Create: `okmeter/core/paths.cpp`
- Test: `okmeter/tests/test_paths.cpp`

**Interfaces:**
- Consumes: tests/framework.h
- Produces: `kimiHome()` / `okmeterDir()` / `pathU8()`——Task 5/6/8 依赖。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_paths.cpp`：

```cpp
#include "../core/paths.h"
#include "framework.h"
#include <cstdlib>

TEST(paths_kimi_home_env_priority) {
  _wputenv(L"KIMI_CODE_HOME=D:\\custom-kimi");
  CHECK(kimiHome() == std::filesystem::path(L"D:\\custom-kimi"));
  _wputenv(L"KIMI_CODE_HOME=");
  CHECK(kimiHome().filename() == ".kimi-code");
}

TEST(paths_okmeter_dir_autocreate) {
  _wputenv(L"USERPROFILE=");
  auto d = okmeterDir();  // USERPROFILE 缺失 → 回落 ./.okryptos/okmeter
  CHECK(d.filename() == "okmeter");
  CHECK(d.parent_path().filename() == ".okryptos");
  std::error_code ec;
  CHECK(std::filesystem::exists(d, ec));
  std::filesystem::remove_all(d.parent_path().parent_path(), ec);
}

TEST(paths_path_u8_roundtrip) {
  CHECK(pathU8(std::filesystem::path("a") / "b.txt") == "a/b.txt");
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `okmeter\build.bat`
Expected: 编译错误 `cannot open include file: '../core/paths.h'`（红）。

- [ ] **Step 3: 实现 paths**

`okmeter/core/paths.h`：

```cpp
// core/paths.h —— home 解析与路径工具
#pragma once
#include <filesystem>
#include <string>

namespace okmeter {

std::filesystem::path kimiHome();    // KIMI_CODE_HOME 优先，否则 ~/.kimi-code
std::filesystem::path okmeterDir();  // ~/.okryptos/okmeter（不存在则创建）
std::string pathU8(const std::filesystem::path& p);  // 正斜杠 UTF-8

} // namespace okmeter
```

`okmeter/core/paths.cpp`：

```cpp
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
```

- [ ] **Step 4: 跑测试确认通过**

Run: `okmeter\build.bat`
Expected: 三个 paths_* 用例全 `ok`，`8 cases, 0 failures`。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): home 解析与 okmeter 数据目录"
```

---

### Task 4: 聚合统计（core/event + core/aggregator）

**Files:**
- Create: `okmeter/core/event.h`
- Create: `okmeter/core/aggregator.h`
- Create: `okmeter/core/aggregator.cpp`
- Test: `okmeter/tests/test_aggregator.cpp`

**Interfaces:**
- Consumes: `json::{Value, parse, dump, num, str}`（Task 2）
- Produces: `UsageEvent` / `Sums` / `Aggregator` / `ModelStat`——Task 5/6/7/8 依赖。口径语义（规格 §2/§3.3）：all=全部累计；today=本机自然日；week=自然周（周一起）；session=**最近活跃 session**（最后一条事件的 sessionId）的累计；模型口径同四项 + lastCallMs。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_aggregator.cpp`：

```cpp
#include "../core/aggregator.h"
#include "framework.h"
#include <ctime>

using namespace okmeter;

static int64_t msOf(int y, int mo, int d, int h, int mi) {
  std::tm t{};
  t.tm_year = y - 1900; t.tm_mon = mo - 1; t.tm_mday = d; t.tm_hour = h; t.tm_min = mi;
  return (int64_t)std::mktime(&t) * 1000;
}

static UsageEvent ev(const char* model, const char* sess, int64_t io,
                     int64_t icr, int64_t icc, int64_t out, int64_t t) {
  UsageEvent e;
  e.model = model; e.sessionId = sess;
  e.inputOther = io; e.inputCacheRead = icr; e.inputCacheCreation = icc; e.output = out;
  e.timeMs = t;
  return e;
}

TEST(agg_all_total_and_breakdown) {
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 200, 10, 5, msOf(2026, 9, 8, 10, 0)));
  a.add(ev("m/b", "s1", 50, 0, 0, 5, msOf(2026, 9, 9, 11, 0)));
  CHECK_EQ(a.all().total(), 370);
  CHECK_EQ(a.all().inputOther, 150);
  CHECK_EQ(a.all().inputCacheRead, 200);
  CHECK_EQ(a.all().output, 10);
}

TEST(agg_today_week_boundaries) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);  // 周三中午，避开 DST 边缘
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, now - 3600000));            // 今天 1 小时前
  a.add(ev("m/a", "s1", 20, 0, 0, 0, now - 86400000));           // 昨天此时
  a.add(ev("m/a", "s1", 40, 0, 0, 0, now - 7 * 86400000));       // 上周此时
  CHECK_EQ(a.today(now).total(), 10);
  CHECK_EQ(a.week(now).total(), 30);   // 今天 + 昨天（同周）
  CHECK_EQ(a.all().total(), 70);
}

TEST(agg_models_grouping_and_recency) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);
  Aggregator a;
  a.add(ev("m/old", "s1", 1, 0, 0, 0, now - 7200000));
  a.add(ev("m/new", "s1", 2, 0, 0, 0, now - 3600000));
  auto ranked = a.modelsByRecency();
  CHECK_EQ(ranked.size(), (size_t)2);
  CHECK(ranked[0] == "m/new");
  CHECK_EQ(a.model("m/new")->lastCallMs, now - 3600000);
  CHECK_EQ(a.modelToday("m/old", now).total(), 1);
}

TEST(agg_session_is_latest_active) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 10, 0, 0, 0, now - 7200000));
  a.add(ev("m/b", "s2", 20, 0, 0, 0, now - 3600000));   // s2 更晚 → 当前会话
  CHECK_EQ(a.session().total(), 20);
  CHECK_EQ(a.modelSession("m/a").total(), 0);           // m/a 在 s2 无量
  CHECK_EQ(a.modelSession("m/b").total(), 20);
}

TEST(agg_json_roundtrip) {
  const int64_t now = msOf(2026, 9, 9, 12, 0);
  Aggregator a;
  a.add(ev("m/a", "s1", 100, 200, 10, 5, now - 3600000));
  a.add(ev("m/b", "s2", 50, 0, 0, 5, now));
  Aggregator b;
  CHECK(b.fromJson(a.toJson()));
  CHECK_EQ(b.all().total(), 370);
  CHECK_EQ(b.today(now).total(), 370);
  CHECK_EQ(b.session().total(), 55);
  CHECK_EQ(b.modelsByRecency()[0], "m/b");
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `okmeter\build.bat`
Expected: 编译错误 `cannot open include file: '../core/aggregator.h'`（红）。

- [ ] **Step 3: 实现 event.h + aggregator**

`okmeter/core/event.h`：

```cpp
// core/event.h —— 统一 usage 事件（采集适配器 → 核心框架的唯一数据契约）
#pragma once
#include <cstdint>
#include <string>

namespace okmeter {

struct UsageEvent {
  std::string model;       // 例 "kimi-code/k3-256k"
  std::string agentId;     // main / agent-N
  std::string sessionId;   // 来源会话（由适配器从落盘路径提取）
  int64_t inputOther = 0;
  int64_t inputCacheRead = 0;
  int64_t inputCacheCreation = 0;
  int64_t output = 0;
  int64_t timeMs = 0;      // epoch 毫秒
  std::string usageScope;  // 原始 scope（turn 等），保留不解析

  int64_t total() const { return inputOther + inputCacheRead + inputCacheCreation + output; }
};

} // namespace okmeter
```

`okmeter/core/aggregator.h`：

```cpp
// core/aggregator.h —— 聚合统计：all/today/week/session × 全局/模型
#pragma once
#include "event.h"
#include "minjson.h"
#include <map>
#include <vector>

namespace okmeter {

struct Sums {
  int64_t inputOther = 0, inputCacheRead = 0, inputCacheCreation = 0, output = 0;
  int64_t total() const { return inputOther + inputCacheRead + inputCacheCreation + output; }
  void plus(const Sums& o) {
    inputOther += o.inputOther; inputCacheRead += o.inputCacheRead;
    inputCacheCreation += o.inputCacheCreation; output += o.output;
  }
  static Sums of(const UsageEvent& e) {
    Sums s;
    s.inputOther = e.inputOther; s.inputCacheRead = e.inputCacheRead;
    s.inputCacheCreation = e.inputCacheCreation; s.output = e.output;
    return s;
  }
};

struct ModelStat {
  Sums all;
  std::map<int, Sums> byDay;              // dayKey(yyyymmdd) → 当日
  std::map<std::string, Sums> bySession;  // sessionId → 该会话
  int64_t lastCallMs = 0;
};

class Aggregator {
public:
  void add(const UsageEvent& e);

  // 全局口径
  Sums all() const { return allAll_; }
  Sums today(int64_t nowMs) const;
  Sums week(int64_t nowMs) const;
  Sums session() const;  // 最近活跃 session 的累计

  // 模型口径
  std::vector<std::string> modelsByRecency() const;
  const ModelStat* model(const std::string& id) const {
    auto it = models_.find(id);
    return it == models_.end() ? nullptr : &it->second;
  }
  Sums modelToday(const std::string& id, int64_t nowMs) const;
  Sums modelWeek(const std::string& id, int64_t nowMs) const;
  Sums modelSession(const std::string& id) const;

  // 序列化（state.json）
  json::Value toJson() const;
  bool fromJson(const json::Value& v);

  static int dayKey(int64_t ms);            // 本地时区 yyyymmdd
  static int weekStartKey(int64_t ms);      // 本周一的 dayKey

private:
  Sums allAll_;
  std::map<int, Sums> dayAll_;
  std::map<std::string, Sums> sessAll_;
  std::map<std::string, ModelStat> models_;
  std::string hotSession_;
  int64_t hotSessionMs_ = -1;
};

} // namespace okmeter
```

`okmeter/core/aggregator.cpp`：

```cpp
#include "aggregator.h"
#include <ctime>

namespace okmeter {
namespace {

json::Value sumsJson(const Sums& s) {
  json::Object o;
  o["io"] = json::num(s.inputOther);
  o["icr"] = json::num(s.inputCacheRead);
  o["icc"] = json::num(s.inputCacheCreation);
  o["o"] = json::num(s.output);
  json::Value v;
  v.v = std::move(o);
  return v;
}

void sumsFrom(const json::Value* v, Sums& s) {
  if (!v || !v->isObject()) return;
  s.inputOther = (int64_t)v->find("io")->num();
  s.inputCacheRead = (int64_t)v->find("icr")->num();
  s.inputCacheCreation = (int64_t)v->find("icc")->num();
  s.output = (int64_t)v->find("o")->num();
}

template <typename Map>
json::Value mapJson(const Map& m) {
  json::Object o;
  for (const auto& [k, s] : m) o[std::to_string(k)] = sumsJson(s);
  json::Value v;
  v.v = std::move(o);
  return v;
}

json::Value strMapJson(const std::map<std::string, Sums>& m) {
  json::Object o;
  for (const auto& [k, s] : m) o[k] = sumsJson(s);
  json::Value v;
  v.v = std::move(o);
  return v;
}

int64_t dayKeyToMs(int key) {
  std::tm t{};
  t.tm_year = key / 10000 - 1900;
  t.tm_mon = (key / 100) % 100 - 1;
  t.tm_mday = key % 100;
  t.tm_hour = 12;  // 正午，避开 DST 切换边缘
  return (int64_t)std::mktime(&t) * 1000;
}

} // namespace

int Aggregator::dayKey(int64_t ms) {
  time_t t = (time_t)(ms / 1000);
  std::tm lt{};
  localtime_s(&lt, &t);
  return (lt.tm_year + 1900) * 10000 + (lt.tm_mon + 1) * 100 + lt.tm_mday;
}

int Aggregator::weekStartKey(int64_t ms) {
  time_t t = (time_t)(ms / 1000);
  std::tm lt{};
  localtime_s(&lt, &t);
  int back = (lt.tm_wday + 6) % 7;  // 周一起
  lt.tm_mday -= back;
  lt.tm_hour = 12;
  time_t monday = std::mktime(&lt);  // mktime 规范化负 mday
  std::tm mlt{};
  localtime_s(&mlt, &monday);
  return (mlt.tm_year + 1900) * 10000 + (mlt.tm_mon + 1) * 100 + mlt.tm_mday;
}

void Aggregator::add(const UsageEvent& e) {
  const Sums s = Sums::of(e);
  allAll_.plus(s);
  dayAll_[dayKey(e.timeMs)].plus(s);
  sessAll_[e.sessionId].plus(s);
  ModelStat& m = models_[e.model];
  m.all.plus(s);
  m.byDay[dayKey(e.timeMs)].plus(s);
  m.bySession[e.sessionId].plus(s);
  if (e.timeMs > m.lastCallMs) m.lastCallMs = e.timeMs;
  if (e.timeMs >= hotSessionMs_) {  // >=：同毫秒后到的覆盖，保证"最近活跃"单调
    hotSessionMs_ = e.timeMs;
    hotSession_ = e.sessionId;
  }
}

Sums Aggregator::today(int64_t nowMs) const {
  auto it = dayAll_.find(dayKey(nowMs));
  return it == dayAll_.end() ? Sums{} : it->second;
}

Sums Aggregator::week(int64_t nowMs) const {
  const int ws = weekStartKey(nowMs);
  Sums out;
  for (const auto& [k, s] : dayAll_)
    if (weekStartKey(dayKeyToMs(k)) == ws) out.plus(s);
  return out;
}

Sums Aggregator::session() const {
  auto it = sessAll_.find(hotSession_);
  return it == sessAll_.end() ? Sums{} : it->second;
}

std::vector<std::string> Aggregator::modelsByRecency() const {
  std::vector<const ModelStat*> tmp;
  std::vector<std::string> out;
  for (const auto& [id, m] : models_) tmp.push_back(&m);
  std::sort(tmp.begin(), tmp.end(),
            [](const ModelStat* a, const ModelStat* b) { return a->lastCallMs > b->lastCallMs; });
  // ModelStat* 无法反查 id，改用 id 收集
  std::vector<std::pair<int64_t, std::string>> keyed;
  for (const auto& [id, m] : models_) keyed.emplace_back(m.lastCallMs, id);
  std::sort(keyed.begin(), keyed.end(),
            [](const auto& a, const auto& b) { return a.first > b.first; });
  for (auto& [_, id] : keyed) out.push_back(id);
  return out;
}

Sums Aggregator::modelToday(const std::string& id, int64_t nowMs) const {
  const ModelStat* m = model(id);
  if (!m) return Sums{};
  auto it = m->byDay.find(dayKey(nowMs));
  return it == m->byDay.end() ? Sums{} : it->second;
}

Sums Aggregator::modelWeek(const std::string& id, int64_t nowMs) const {
  const ModelStat* m = model(id);
  if (!m) return Sums{};
  const int ws = weekStartKey(nowMs);
  Sums out;
  for (const auto& [k, s] : m->byDay)
    if (weekStartKey(dayKeyToMs(k)) == ws) out.plus(s);
  return out;
}

Sums Aggregator::modelSession(const std::string& id) const {
  const ModelStat* m = model(id);
  if (!m) return Sums{};
  auto it = m->bySession.find(hotSession_);
  return it == m->bySession.end() ? Sums{} : it->second;
}

json::Value Aggregator::toJson() const {
  json::Object root;
  root["all"] = sumsJson(allAll_);
  root["days"] = mapJson(dayAll_);
  root["sessions"] = strMapJson(sessAll_);
  root["hot"] = json::str(hotSession_);
  root["hotMs"] = json::num(hotSessionMs_);
  json::Object models;
  for (const auto& [id, m] : models_) {
    json::Object mo;
    mo["all"] = sumsJson(m.all);
    mo["days"] = mapJson(m.byDay);
    mo["sessions"] = strMapJson(m.bySession);
    mo["last"] = json::num(m.lastCallMs);
    json::Value mv;
    mv.v = std::move(mo);
    models[id] = std::move(mv);
  }
  json::Value msv;
  msv.v = std::move(models);
  root["models"] = std::move(msv);
  json::Value v;
  v.v = std::move(root);
  return v;
}

bool Aggregator::fromJson(const json::Value& v) {
  if (!v.isObject()) return false;
  sumsFrom(v.find("all"), allAll_);
  for (const auto& [k, s] : v.find("days")->obj())
    sumsFrom(&s, dayAll_[std::stoi(k)]);
  for (const auto& [k, s] : v.find("sessions")->obj())
    sumsFrom(&s, sessAll_[k]);
  hotSession_ = v.find("hot")->str();
  hotSessionMs_ = (int64_t)v.find("hotMs")->num(-1);
  for (const auto& [id, mv] : v.find("models")->obj()) {
    ModelStat m;
    sumsFrom(mv.find("all"), m.all);
    for (const auto& [k, s] : mv.find("days")->obj())
      sumsFrom(&s, m.byDay[std::stoi(k)]);
    for (const auto& [k, s] : mv.find("sessions")->obj())
      sumsFrom(&s, m.bySession[k]);
    m.lastCallMs = (int64_t)mv.find("last")->num();
    models_[id] = std::move(m);
  }
  return true;
}

} // namespace okmeter
```

注意：`aggregator.cpp` 需要 `#include <algorithm>`（std::sort）与 `<utility>`——加在 include 区。`modelsByRecency` 里的 `tmp` 收集是冗余，只保留 keyed 分支即可（实现时删除 tmp 三行）。

- [ ] **Step 4: 跑测试确认通过**

Run: `okmeter\build.bat`
Expected: 五个 agg_* 用例全 `ok`，`13 cases, 0 failures`。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 统一事件模型与聚合统计（all/today/week/session × 全局/模型）"
```

---

### Task 5: 游标持久化（core/store）

**Files:**
- Create: `okmeter/core/store.h`
- Create: `okmeter/core/store.cpp`
- Test: `okmeter/tests/test_store.cpp`

**Interfaces:**
- Consumes: `Aggregator`（Task 4）、`json`（Task 2）
- Produces: `Store`——Task 6/8 依赖。规格 §2：游标与累计结果同存 `state.json`，原子写入。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_store.cpp`：

```cpp
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path tempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

TEST(store_cursor_roundtrip) {
  auto d = tempDir("cursor");
  {
    Store s(d);
    s.setCursor("a/wire.jsonl", 1234);
    CHECK(s.flush());
  }
  {
    Store s(d);
    CHECK(s.load());
    CHECK_EQ(s.cursor("a/wire.jsonl"), 1234);
    CHECK_EQ(s.cursor("never/seen"), 0);
  }
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(store_agg_snapshot_roundtrip) {
  auto d = tempDir("agg");
  {
    Store s(d);
    UsageEvent e;
    e.model = "m/a"; e.sessionId = "s1";
    e.inputOther = 100; e.inputCacheRead = 50;
    e.timeMs = 1788865482021LL;
    s.agg().add(e);
    CHECK(s.flush());
  }
  {
    Store s(d);
    CHECK(s.load());
    CHECK_EQ(s.agg().all().total(), 150);
    CHECK_EQ(s.agg().modelsByRecency()[0], "m/a");
  }
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(store_corrupted_state_falls_back_fresh) {
  auto d = tempDir("broken");
  {
    std::ofstream f(d / "state.json", std::ios::binary);
    f << "{broken json";
  }
  Store s(d);
  CHECK(!s.load());                       // 损坏 → false，内部保持全新
  CHECK_EQ(s.cursor("x"), 0);
  CHECK_EQ(s.agg().all().total(), 0);
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
}

TEST(store_flush_is_atomic_no_tmp_left) {
  auto d = tempDir("atomic");
  Store s(d);
  s.setCursor("f", 1);
  CHECK(s.flush());
  std::error_code ec;
  CHECK(std::filesystem::exists(d / "state.json", ec));
  CHECK(!std::filesystem::exists(d / "state.json.tmp", ec));  // tmp 已被替换走
  std::filesystem::remove_all(d, ec);
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `okmeter\build.bat`
Expected: 编译错误 `cannot open include file: '../core/store.h'`（红）。

- [ ] **Step 3: 实现 store**

`okmeter/core/store.h`：

```cpp
// core/store.h —— 游标 + 聚合快照持久化（state.json，原子写）
#pragma once
#include "aggregator.h"
#include <filesystem>
#include <map>
#include <string>

namespace okmeter {

class Store {
public:
  explicit Store(std::filesystem::path dir) : dir_(std::move(dir)) {}

  int64_t cursor(const std::string& file) const {
    auto it = cursors_.find(file);
    return it == cursors_.end() ? 0 : it->second;
  }
  void setCursor(const std::string& file, int64_t off) { cursors_[file] = off; }

  Aggregator& agg() { return agg_; }

  bool load();   // 读 state.json；缺失/损坏 → false，内部状态保持全新
  bool flush();  // 原子写：state.json.tmp → MoveFileExW(REPLACE_EXISTING)

private:
  std::filesystem::path dir_;
  std::map<std::string, int64_t> cursors_;
  Aggregator agg_;
};

} // namespace okmeter
```

`okmeter/core/store.cpp`：

```cpp
#include "store.h"
#include <fstream>

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

namespace okmeter {

bool Store::load() {
  std::ifstream in(dir_ / "state.json", std::ios::binary);
  if (!in) return false;
  std::string text((std::istreambuf_iterator<char>(in)), std::istreambuf_iterator<char>());
  json::Value v;
  if (!json::parse(text, v) || !v.isObject()) return false;
  for (const auto& [k, off] : v.find("cursors")->obj())
    cursors_[k] = (int64_t)off.num();
  agg_.fromJson(*v.find("agg"));   // find 可能为 nullptr？不会：v 是对象但缺 agg 键时
  return true;                     // find 返回 nullptr —— 需要防护，见下
}

bool Store::flush() {
  std::error_code ec;
  std::filesystem::create_directories(dir_, ec);
  json::Object root;
  json::Object cur;
  for (const auto& [k, off] : cursors_) cur[k] = json::num(off);
  json::Value cv;
  cv.v = std::move(cur);
  root["cursors"] = std::move(cv);
  root["agg"] = agg_.toJson();
  json::Value rv;
  rv.v = std::move(root);

  const auto tmp = dir_ / "state.json.tmp";
  {
    std::ofstream out(tmp, std::ios::binary | std::ios::trunc);
    if (!out) return false;
    out << json::dump(rv);
    out.flush();
    if (!out) return false;
  }
  const auto dst = dir_ / "state.json";
  return MoveFileExW(tmp.c_str(), dst.c_str(),
                     MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH) != 0;
}

} // namespace okmeter
```

注意 `load()` 中 `v.find("agg")` 可能返回 nullptr（老版本 state.json 缺键），`agg_.fromJson(*nullptr)` 是 UB。实现时改为：

```cpp
  if (const json::Value* a = v.find("agg")) agg_.fromJson(*a);
  return true;
```

同理 `v.find("cursors")->obj()` 对 nullptr 解引用——`find` 返回 nullptr 时 `->obj()` 同样 UB，改为：

```cpp
  if (const json::Value* c = v.find("cursors"))
    for (const auto& [k, off] : c->obj())
      cursors_[k] = (int64_t)off.num();
```

（计划中这两处防护必须照写进最终实现，测试 `store_corrupted_state_falls_back_fresh` 只覆盖整文件损坏，但缺键防护是同一纪律。）

- [ ] **Step 4: 跑测试确认通过**

Run: `okmeter\build.bat`
Expected: 四个 store_* 用例全 `ok`，`17 cases, 0 failures`。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 游标与聚合快照的原子化持久化"
```

---

### Task 6: Kimi 采集适配器（adapters/kimi）

**Files:**
- Create: `okmeter/adapters/adapter.h`
- Create: `okmeter/adapters/kimi/adapter.h`
- Create: `okmeter/adapters/kimi/adapter.cpp`
- Delete: `okmeter/adapters/kimi/placeholder.cpp`
- Test: `okmeter/tests/test_kimi_adapter.cpp`

**Interfaces:**
- Consumes: `Store`（Task 5）、`json`（Task 2）、`pathU8`（Task 3）
- Produces: `IAdapter` / `EventSink` / `KimiAdapter`——Task 7/8 依赖。规格 §3.7：全量扫描 + 游标增量读；坏行/未知 type/缺 usage 跳过；`~/.kimi-code` 只读。

- [ ] **Step 1: 写失败测试**

`okmeter/tests/test_kimi_adapter.cpp`：

```cpp
#include "../adapters/kimi/adapter.h"
#include "../core/store.h"
#include "framework.h"
#include <fstream>
#include <process.h>

using namespace okmeter;

static std::filesystem::path tempDir(const char* name) {
  auto d = std::filesystem::temp_directory_path() /
           ("okmeter-test-" + std::string(name) + "-" + std::to_string(_getpid()));
  std::error_code ec;
  std::filesystem::remove_all(d, ec);
  std::filesystem::create_directories(d);
  return d;
}

static void mkfile(const std::filesystem::path& p, const std::string& content) {
  std::filesystem::create_directories(p.parent_path());
  std::ofstream f(p, std::ios::binary | std::ios::trunc);
  f << content;
}

static const char* kGood1 =
  R"({"type":"usage.record","agentId":"main","model":"m/a","usage":{"inputOther":100,"output":10,"inputCacheRead":50,"inputCacheCreation":5},"usageScope":"turn","time":1788865482021})";
static const char* kGood2 =
  R"({"type":"usage.record","agentId":"main","model":"m/b","usage":{"inputOther":200,"output":20,"inputCacheRead":0,"inputCacheCreation":0},"usageScope":"turn","time":1788865483021})";

TEST(kimi_scan_increment_and_tolerance) {
  auto home = tempDir("kimi-home");
  auto stateDir = tempDir("kimi-state");
  const auto f1 = home / "sessions" / "wd_abc" / "session_1111" / "agents" / "main" / "wire.jsonl";
  const auto f2 = home / "sessions" / "wd_abc" / "session_1111" / "agents" / "agent-1" / "wire.jsonl";
  const auto f3 = home / "sessions" / "wd_def" / "session_2222" / "agents" / "main" / "wire.jsonl";
  mkfile(f1, std::string(kGood1) + "\n{bad json\n" +
             R"({"type":"other","x":1})" "\n" + kGood2 + "\n");
  mkfile(f2, std::string(kGood1) + "\n");
  mkfile(f3, std::string(R"({"type":"usage.record","model":"m/c"})") + "\n" +  // 缺 usage → 跳过
             std::string(kGood1));  // 结尾无换行的半行 → 留给下一轮

  Store store(stateDir);
  KimiAdapter kimi(home, &store);
  Aggregator agg;
  std::string lastSession;
  int n = kimi.poll([&](const UsageEvent& e) { agg.add(e); lastSession = e.sessionId; });

  CHECK_EQ(n, 4);                       // f1 两条 + f2 一条 + f3 零条完整好行
  CHECK_EQ(agg.all().total(), 1030);    // (100+10+50+5)*3 + (200+20)
  CHECK_EQ(agg.model("m/a")->all.total(), 495);
  CHECK_EQ(agg.model("m/b")->all.total(), 220);
  CHECK(lastSession == "session_1111"); // sessionId 从路径提取

  // 第二轮：补齐半行 + 追加一条新行 → 只增量产出 2 条
  {
    std::ofstream f(f3, std::ios::binary | std::ios::app);
    f << "\n" << kGood2 << "\n";
  }
  int n2 = kimi.poll([&](const UsageEvent& e) { agg.add(e); });
  CHECK_EQ(n2, 2);
  CHECK_EQ(agg.all().total(), 1030 + 165 + 220);

  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}

TEST(kimi_empty_home_no_crash) {
  auto home = tempDir("kimi-empty");
  auto stateDir = tempDir("kimi-empty-state");
  Store store(stateDir);
  KimiAdapter kimi(home, &store);
  int n = kimi.poll([](const UsageEvent&) {});
  CHECK_EQ(n, 0);
  std::error_code ec;
  std::filesystem::remove_all(home, ec);
  std::filesystem::remove_all(stateDir, ec);
}
```

预期值核算：kGood1 total = 100+10+50+5 = 165；kGood2 total = 220。第一轮 f1 产出 165+220、f2 产出 165、f3 半行不产出 → n=4、总量 165*3+220=715？**注意：165*3+220 = 715，上面写的 1030 是错的**，实现测试时改为 `CHECK_EQ(agg.all().total(), 715)`；`m/a` 三条 165*3=495 ✓。第二轮：半行补齐（165）+ 新行（220）→ n2=2，总量 715+385=1100，断言写 `CHECK_EQ(agg.all().total(), 1100)`。

- [ ] **Step 2: 跑测试确认失败**

Run: `okmeter\build.bat`
Expected: 编译错误 `cannot open include file: '../adapters/kimi/adapter.h'`（红）。

- [ ] **Step 3: 实现 IAdapter + KimiAdapter**

`okmeter/adapters/adapter.h`：

```cpp
// adapters/adapter.h —— 采集适配器接口：每种 agent 工具一个实现
#pragma once
#include "../core/event.h"
#include <functional>
#include <string>

namespace okmeter {

using EventSink = std::function<void(const UsageEvent&)>;

class IAdapter {
public:
  virtual ~IAdapter() = default;
  virtual std::string id() const = 0;            // 例 "kimi-code"
  virtual int poll(const EventSink& sink) = 0;   // 全量/增量扫描，返回本轮产出事件数
};

} // namespace okmeter
```

`okmeter/adapters/kimi/adapter.h`：

```cpp
// adapters/kimi/adapter.h —— Kimi Code 适配器：sessions/**/wire.jsonl 游标增量扫描
#pragma once
#include "../adapter.h"
#include <filesystem>

namespace okmeter {

class Store;

class KimiAdapter : public IAdapter {
public:
  KimiAdapter(std::filesystem::path kimiHome, Store* store);
  std::string id() const override { return "kimi-code"; }
  int poll(const EventSink& sink) override;

private:
  int tailFile(const std::filesystem::path& f, const EventSink& sink);
  bool parseLine(std::string_view line, const std::string& sessionId,
                 const EventSink& sink);
  std::filesystem::path home_;
  Store* store_;
};

} // namespace okmeter
```

`okmeter/adapters/kimi/adapter.cpp`：

```cpp
#include "adapter.h"
#include "../../core/minjson.h"
#include "../../core/paths.h"
#include "../../core/store.h"
#include <fstream>

namespace okmeter {
namespace {

std::string sessionIdOf(const std::filesystem::path& p) {
  for (const auto& c : p) {
    std::string s = pathU8(c);
    if (s.rfind("session_", 0) == 0) return s;
  }
  return "";
}

} // namespace

KimiAdapter::KimiAdapter(std::filesystem::path kimiHome, Store* store)
  : home_(std::move(kimiHome)), store_(store) {}

int KimiAdapter::poll(const EventSink& sink) {
  int emitted = 0;
  const auto root = home_ / "sessions";
  std::error_code ec;
  if (!std::filesystem::exists(root, ec)) return 0;
  for (std::filesystem::recursive_directory_iterator it(root, ec), endIt;
       !ec && it != endIt; it.increment(ec)) {
    std::error_code ec2;
    if (it->is_regular_file(ec2) && !ec2 && it->path().filename() == "wire.jsonl")
      emitted += tailFile(it->path(), sink);
  }
  return emitted;
}

int KimiAdapter::tailFile(const std::filesystem::path& f, const EventSink& sink) {
  const std::string key = pathU8(f);
  int64_t off = store_->cursor(key);
  std::error_code ec;
  const auto size = (int64_t)std::filesystem::file_size(f, ec);
  if (ec) return 0;
  if (size < off) off = 0;        // 文件被截断/轮换：从头读
  if (size == off) return 0;

  std::ifstream in(f, std::ios::binary);
  if (!in) return 0;
  in.seekg(off);
  std::string chunk((std::istreambuf_iterator<char>(in)), std::istreambuf_iterator<char>());

  // 游标只推进到最后一个完整行；结尾半行留给下一轮
  const size_t lastNl = chunk.rfind('\n');
  if (lastNl == std::string::npos) return 0;
  const std::string_view work(chunk.data(), lastNl + 1);
  const int64_t newOff = off + (int64_t)work.size();

  const std::string sessionId = sessionIdOf(f);
  int emitted = 0;
  size_t pos = 0;
  while (pos < work.size()) {
    const size_t nl = work.find('\n', pos);
    parseLine(work.substr(pos, nl - pos), sessionId, sink) && ++emitted;
    pos = nl + 1;
  }
  store_->setCursor(key, newOff);
  return emitted;
}

bool KimiAdapter::parseLine(std::string_view line, const std::string& sessionId,
                            const EventSink& sink) {
  json::Value v;
  if (!json::parse(std::string(line), v)) return false;        // 坏行跳过
  const json::Value* type = v.find("type");
  if (!type || type->str() != "usage.record") return false;    // 未知 type 跳过
  const json::Value* usage = v.find("usage");
  if (!usage || !usage->isObject()) return false;              // 缺 usage 跳过
  const json::Value* model = v.find("model");
  if (!model || model->str().empty()) return false;

  UsageEvent e;
  e.model = model->str();
  if (const json::Value* a = v.find("agentId")) e.agentId = a->str();
  if (const json::Value* s = v.find("usageScope")) e.usageScope = s->str();
  e.sessionId = sessionId;
  if (const json::Value* x = usage->find("inputOther")) e.inputOther = (int64_t)x->num();
  if (const json::Value* x = usage->find("inputCacheRead")) e.inputCacheRead = (int64_t)x->num();
  if (const json::Value* x = usage->find("inputCacheCreation")) e.inputCacheCreation = (int64_t)x->num();
  if (const json::Value* x = usage->find("output")) e.output = (int64_t)x->num();
  if (const json::Value* t = v.find("time")) e.timeMs = (int64_t)t->num();
  sink(e);
  return true;
}

} // namespace okmeter
```

注意 `parseLine(...) && ++emitted;` 是未定义行为边缘写法（前置自增作右操作数），实现时改为直白形式：

```cpp
    if (parseLine(work.substr(pos, nl - pos), sessionId, sink)) ++emitted;
```

删除 `okmeter/adapters/kimi/placeholder.cpp`。

- [ ] **Step 4: 跑测试确认通过**

Run: `okmeter\build.bat`
Expected: 两个 kimi_* 用例全 `ok`，`19 cases, 0 failures`。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): Kimi Code 采集适配器（游标增量 + 行级容错）"
```

---

### Task 7: 模块注册表与适配器契约测试（core/registry）

**Files:**
- Create: `okmeter/core/registry.h`
- Test: `okmeter/tests/test_registry.cpp`

**Interfaces:**
- Consumes: `IAdapter`（Task 6）、`Aggregator`（Task 4）
- Produces: `Registry<T>`——Plan 2 渲染模块（形态/材质）复用同一注册表。规格 §3.6：新增监听对象 = 新增适配器模块，框架代码零改动——本条由契约测试兜底。

- [ ] **Step 1: 写失败测试（含伪造适配器）**

`okmeter/tests/test_registry.cpp`：

```cpp
#include "../core/registry.h"
#include "../adapters/adapter.h"
#include "../core/aggregator.h"
#include "framework.h"

using namespace okmeter;

namespace {

// 伪造适配器：不读盘，直接吐两条合成事件——验证"新增适配器零改动框架"
class FakeAdapter : public IAdapter {
public:
  std::string id() const override { return "fake-agent"; }
  int poll(const EventSink& sink) override {
    UsageEvent a;
    a.model = "fake/m1"; a.sessionId = "fs1";
    a.inputOther = 100; a.timeMs = 1000;
    sink(a);
    UsageEvent b;
    b.model = "fake/m2"; b.sessionId = "fs1";
    b.output = 200; b.timeMs = 2000;
    sink(b);
    return 2;
  }
};

} // namespace

TEST(registry_contract_new_adapter_needs_no_framework_change) {
  Registry<IAdapter> reg;
  reg.add("fake-agent", [] { return std::make_unique<FakeAdapter>(); });

  auto a = reg.create("fake-agent");
  CHECK(a != nullptr);
  CHECK(a->id() == "fake-agent");

  Aggregator agg;
  int n = a->poll([&](const UsageEvent& e) { agg.add(e); });
  CHECK_EQ(n, 2);
  CHECK_EQ(agg.all().total(), 300);
  CHECK_EQ(agg.modelsByRecency()[0], "fake/m2");

  CHECK(reg.create("nonexistent") == nullptr);
  auto names = reg.names();
  CHECK_EQ(names.size(), (size_t)1);
  CHECK(names[0] == "fake-agent");
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `okmeter\build.bat`
Expected: 编译错误 `cannot open include file: '../core/registry.h'`（红）。

- [ ] **Step 3: 实现 registry.h（头文件模板）**

`okmeter/core/registry.h`：

```cpp
// core/registry.h —— 模块注册表：采集适配器与渲染模块（Plan 2 形态/材质）通用
#pragma once
#include <functional>
#include <map>
#include <memory>
#include <string>
#include <vector>

namespace okmeter {

template <typename T>
class Registry {
public:
  using Factory = std::function<std::unique_ptr<T>()>;

  void add(const std::string& name, Factory f) { factories_[name] = std::move(f); }

  std::unique_ptr<T> create(const std::string& name) const {
    auto it = factories_.find(name);
    return it == factories_.end() ? nullptr : it->second();
  }

  std::vector<std::string> names() const {
    std::vector<std::string> out;
    out.reserve(factories_.size());
    for (const auto& [k, _] : factories_) out.push_back(k);
    return out;
  }

private:
  std::map<std::string, Factory> factories_;
};

} // namespace okmeter
```

- [ ] **Step 4: 跑测试确认通过**

Run: `okmeter\build.bat`
Expected: registry 用例 `ok`，`20 cases, 0 failures`。

- [ ] **Step 5: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 模块注册表与新增适配器零改动契约测试"
```

---

### Task 8: 冒烟 exe（真实扫描）与集成验收

**Files:**
- Modify: `okmeter/app/main.cpp`（占位 → 真扫描）
- 验证：`okmeter\build.bat` 全链路

**Interfaces:**
- Consumes: `Store` / `KimiAdapter` / `Aggregator` / `kimiHome` / `okmeterDir` / `pathU8`（Task 3~6）
- Produces: `build\OkMeter.exe`：扫一次真实 `~/.kimi-code`，打印各口径总量与按最近使用排序的模型列表；同时把 state.json 落盘（第二次运行输出 `+ 0 new usage records` 证明游标生效）。

- [ ] **Step 1: 实现冒烟入口**

`okmeter/app/main.cpp` 全文替换为：

```cpp
// app/main.cpp —— 冒烟入口（Plan 1）：扫描真实 home 打印各口径总量
// 窗口/渲染属 Plan 2；本入口同时充当采集链路的可执行验收
#include "../adapters/kimi/adapter.h"
#include "../core/aggregator.h"
#include "../core/paths.h"
#include "../core/store.h"
#include <chrono>
#include <cstdio>

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

using namespace okmeter;

static int64_t nowMs() {
  return std::chrono::duration_cast<std::chrono::milliseconds>(
      std::chrono::system_clock::now().time_since_epoch()).count();
}

static void printSums(const char* label, const Sums& s) {
  std::printf("%-8s %lld tokens (in %lld / cache-read %lld / cache-create %lld / out %lld)\n",
              label, (long long)s.total(), (long long)s.inputOther,
              (long long)s.inputCacheRead, (long long)s.inputCacheCreation,
              (long long)s.output);
}

int main() {
  SetConsoleOutputCP(CP_UTF8);
  Store store(okmeterDir());
  store.load();
  KimiAdapter kimi(kimiHome(), &store);
  Aggregator& agg = store.agg();
  const int n = kimi.poll([&](const UsageEvent& e) { agg.add(e); });
  store.flush();

  std::printf("kimi-home: %s\n+ %d new usage records\n\n",
              pathU8(kimiHome()).c_str(), n);
  const int64_t now = nowMs();
  printSums("session", agg.session());
  printSums("today", agg.today(now));
  printSums("week", agg.week(now));
  printSums("all", agg.all());
  std::printf("\nmodels by recency:\n");
  for (const std::string& id : agg.modelsByRecency()) {
    const ModelStat* m = agg.model(id);
    std::printf("  %-24s all %lld\n", id.c_str(), (long long)m->all.total());
  }
  return 0;
}
```

- [ ] **Step 2: 构建 + 跑单测**

Run: `okmeter\build.bat`
Expected: `20 cases, 0 failures` → `完成：build\OkMeter.exe`。

- [ ] **Step 3: 冒烟验收（人工，命令行可复核）**

Run: `okmeter\build\OkMeter.exe`（连续跑两次）
Expected:
- 第一次：`+ N new usage records`（N>0，前提：本机存在 `~/.kimi-code/sessions/**/wire.jsonl`），`all` 行为全量累计；
- 第二次：`+ 0 new usage records` 且 `all` 行数值与第一次完全一致（游标 + state.json 生效，总量不回退）；
- `models by recency` 首行是最近真实使用的模型。

- [ ] **Step 4: Commit**

```bash
git add okmeter
git commit -m "feat(okmeter): 冒烟入口——真实扫描打印各口径总量"
```

---

## 后续计划（不在本计划范围）

- **Plan 2（渲染层）**：DComp 透明窗口 + D3D11 管线 + WGC 背景捕获（`WDA_EXCLUDEFROMCAPTURE`）+ 弹簧动效 + 形态（球体弧线/胶囊量表/星环罗盘）与材质（暗夜/毛玻璃/液态玻璃/沉浸光感）渲染模块，复用本计划的 `Registry<T>` 与 `Aggregator` 查询接口。
- **Plan 3（设置背板与交互）**：D2D/DWrite 自绘背板面板、右键菜单、悬停详情卡、两段式保存。
- **Plan 4（集成与分发）**：`internal/tray/tray_windows.go` 菜单项（idMenu 1003，仿 `open_windows.go:47-59` 同目录拉起）、`scripts/build-dist.sh` 加 MSVC 步骤、`version.rc` 接 `sync-version.sh`（注意既有 sed 易碎教训）、`installer/okryptos.iss` [Files] 段、子系统切 `/SUBSYSTEM:WINDOWS`。

## Self-Review 记录

- 规格覆盖：§2 事件模型/home 解析/游标 → Task 2/3/5；§3.6 三层与注册表 → Task 7；§3.7 扫描/容错/恢复 → Task 6；§6 数据层单测 + 契约测试 → 全部 Task 的测试 + Task 7。§3.7 的 `ReadDirectoryChangesW` 运行期监听属窗口进程职责，归入 Plan 2（本计划 poll 接口即轮询兜底，语义兼容）。
- 已知有意留白的修正项都以内联"注意"标注在对应任务里（agg 测试预期值 715/1100、store 的 find 缺键防护、parseLine 自增写法、modelsByRecency 冗余收集）——执行时必须照注意项落地。
- 类型一致性：`EventSink`/`IAdapter::poll` 签名在 Task 6/7/8 一致；`Aggregator` 查询签名与 Task 4 头文件一致；`pathU8` 在 Task 3 定义、6/8 使用。
