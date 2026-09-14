#include "../core/provider.h"
#include "framework.h"

using namespace okmeter;

TEST(provider_known_families) {
  // 月之暗面（kimi 系：k3/k3-256k/kimi-for-coding/k2/k1.5）
  CHECK(providerOf("kimi-code/k3") == "月之暗面");
  CHECK(providerOf("dsh/k3-256k") == "月之暗面");
  CHECK(providerOf("kimi-code/kimi-for-coding") == "月之暗面");
  CHECK(providerOf("codex/k3") == "月之暗面");
  CHECK(providerOf("zcode/K3-256K") == "月之暗面");  // 大小写不敏感
  // OpenAI
  CHECK(providerOf("codex/gpt-5.5") == "OpenAI");
  CHECK(providerOf("codex/gpt-5.2") == "OpenAI");
  // DeepSeek
  CHECK(providerOf("codex/deepseek-v4-pro") == "DeepSeek");
  CHECK(providerOf("reasonix/deepseek-v4-flash") == "DeepSeek");
  // 通义
  CHECK(providerOf("qwen-code/qwen3.6-plus") == "通义");
  // 混元
  CHECK(providerOf("workbuddy/hy3") == "混元");
  CHECK(providerOf("workbuddy/hy4-preview") == "混元");
  // 智谱（真实命名空间是 zhipuai-coding-plan，也归智谱）
  CHECK(providerOf("zhipuai-coding-plan/glm-5.3") == "智谱");
  CHECK(providerOf("zcode/GLM-5.3-Flash") == "智谱");
  // Anthropic / Google
  CHECK(providerOf("claude-code/claude-opus-4.1") == "Anthropic");
  CHECK(providerOf("foo/gemini-3-pro") == "Google");
}

TEST(provider_fallback_namespace) {
  // 未命中模式：回退命名空间段（旧行为）；无 / 则全名小写化前的原名
  CHECK(providerOf("acme/whatever-x") == "acme");
  CHECK(providerOf("solo-model") == "solo-model");
}
