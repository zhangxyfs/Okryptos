// render/catalog.h —— 形态/材质模块目录（纯数据，无 D3D 依赖，单测可直接包含）。
// 单一事实源：ui 设置面板的选项卡枚举（id/显示名/副标题/缩略图种类）与
// app::createModules 的 id→模块 分发都经这张表；新增形态/材质 = 实现文件 +
// 本表一行 + catalog.cpp 工厂数组一项（顺序一一对应，static_assert 防错位）。
// config 持久化的 id 字符串语义不变；未收录 id 由调用方按既有语义兜底（arc/dark）。
#pragma once

#include <memory>
#include <string>

namespace okmeter::render {

class IForm;
class IMaterial;

// 模块元数据：id（config 持久化字符串）、显示名、副标题（无 → nullptr）、
// 缩略图种类（ui/settings drawThumb 的 idx 语义，矢量绘制留在面板侧按此分派）
struct ModuleMeta {
  const char* id;
  const wchar_t* name;
  const wchar_t* sub;
  int thumb;
};

// 视觉形态（thumb：0=弧线 1=胶囊 2=罗盘）
inline constexpr ModuleMeta kFormCatalog[] = {
    {"arc", L"球体弧线", nullptr, 0},
    {"capsule", L"胶囊量表", nullptr, 1},
    {"compass", L"星环罗盘", nullptr, 2},
};
inline constexpr int kFormCount = (int)(sizeof(kFormCatalog) / sizeof(kFormCatalog[0]));

// 材质效果（thumb：0=dark 1=frost 2=liquid 3=glow）
inline constexpr ModuleMeta kMaterialCatalog[] = {
    {"dark", L"暗夜仪表", L"纯暗色 · 发丝线", 0},
    {"frost", L"毛玻璃", L"乳白磨砂 · 强模糊", 1},
    {"liquid", L"液态玻璃", L"边缘折射 · 高光随指针", 2},
    {"glow", L"沉浸光感", L"通透毛玻璃 · 按压光晕 · 粒子汇聚", 3},
};
inline constexpr int kMaterialCount =
    (int)(sizeof(kMaterialCatalog) / sizeof(kMaterialCatalog[0]));

// 按 id 查目录项；未收录 → nullptr（调用方兜底）
inline const ModuleMeta* findForm(const std::string& id) {
  for (const ModuleMeta& m : kFormCatalog)
    if (id == m.id) return &m;
  return nullptr;
}
inline const ModuleMeta* findMaterial(const std::string& id) {
  for (const ModuleMeta& m : kMaterialCatalog)
    if (id == m.id) return &m;
  return nullptr;
}

// 按 id 实例化模块；未收录 → nullptr（实现见 catalog.cpp）
std::unique_ptr<IForm> createForm(const std::string& id);
std::unique_ptr<IMaterial> createMaterial(const std::string& id);

} // namespace okmeter::render
