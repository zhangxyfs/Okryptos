// core/paths.h —— home 解析与路径工具
#pragma once
#include <filesystem>
#include <string>

namespace okmeter {

std::filesystem::path kimiHome();    // KIMI_CODE_HOME 优先，否则 ~/.kimi-code
std::filesystem::path claudeHome();  // CLAUDE_CONFIG_HOME 优先，否则 ~/.claude
std::filesystem::path codexHome();   // CODEX_HOME 优先，否则 ~/.codex
std::filesystem::path qwenHome();    // ~/.qwen
std::filesystem::path zcodeHome();   // OK_ZCODE_HOME 优先（同 agentx 约定），否则 ~/.zcode
std::filesystem::path okmeterDir();  // ~/.okryptos/okmeter（不存在则创建）
std::string pathU8(const std::filesystem::path& p);  // 正斜杠 UTF-8

} // namespace okmeter
