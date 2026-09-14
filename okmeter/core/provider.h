// core/provider.h —— 模型提供商归组：modelId 形如 <命名空间>/<模型名>，命名空间是
// agent 工具（kimi-code/codex/zcode…）而非提供商。级联菜单按真实提供商分组
//（月之暗面旗下 k3/k3-256k/kimi-for-coding 等），按模型名模式推导，未命中回退
// 命名空间段原样（旧行为）
#pragma once
#include <string>

namespace okmeter {

// 提供商显示名（级联菜单二级分组标题）：按 modelId 最后一段（模型名）模式匹配
std::string providerOf(const std::string& modelId);

} // namespace okmeter
