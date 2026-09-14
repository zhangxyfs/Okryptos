#include "provider.h"

namespace okmeter {
namespace {

std::string lower(std::string s) {
  for (auto& c : s)
    if (c >= 'A' && c <= 'Z') c = (char)(c - 'A' + 'a');
  return s;
}

bool startsWith(const std::string& s, const char* p) {
  const size_t n = std::char_traits<char>::length(p);
  return s.size() >= n && s.compare(0, n, p) == 0;
}

} // namespace

std::string providerOf(const std::string& modelId) {
  const size_t slash = modelId.rfind('/');
  const std::string ns =
      slash == std::string::npos ? std::string() : modelId.substr(0, slash);
  const std::string name =
      lower(slash == std::string::npos ? modelId : modelId.substr(slash + 1));
  if (startsWith(name, "kimi") || startsWith(name, "k1") ||
      startsWith(name, "k2") || startsWith(name, "k3"))
    return "月之暗面";
  if (startsWith(name, "gpt") || startsWith(name, "o1") ||
      startsWith(name, "o3") || startsWith(name, "o4") ||
      startsWith(name, "codex"))
    return "OpenAI";
  if (startsWith(name, "deepseek")) return "DeepSeek";
  if (startsWith(name, "qwen") || startsWith(name, "qwq")) return "通义";
  if (startsWith(name, "hunyuan") ||
      (name.size() > 2 && name[0] == 'h' && name[1] == 'y' && name[2] >= '0' &&
       name[2] <= '9'))
    return "混元";
  if (startsWith(name, "glm")) return "智谱";
  if (startsWith(name, "claude")) return "Anthropic";
  if (startsWith(name, "gemini")) return "Google";
  if (startsWith(name, "doubao")) return "豆包";
  return ns.empty() ? name : ns;  // 未命中：回退命名空间段（旧行为）
}

} // namespace okmeter
