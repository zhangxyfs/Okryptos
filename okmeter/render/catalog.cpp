// render/catalog.cpp —— 目录 id → 模块工厂 分发。工厂数组顺序与 catalog.h
// kFormCatalog/kMaterialCatalog 一一对应（static_assert 防错位）。
#include "catalog.h"
#include "form.h"
#include "material.h"

namespace okmeter::render {
namespace {

using FormFactory = std::unique_ptr<IForm> (*)();
using MaterialFactory = std::unique_ptr<IMaterial> (*)();

constexpr FormFactory kFormFactories[] = {
    &createArcForm,
    &createCapsuleForm,
    &createCompassForm,
};
constexpr MaterialFactory kMaterialFactories[] = {
    &createDarkMaterial,
    &createFrostMaterial,
    &createLiquidMaterial,
    &createGlowMaterial,
};
static_assert(sizeof(kFormFactories) / sizeof(kFormFactories[0]) == (size_t)kFormCount,
              "kFormFactories 与 kFormCatalog 数量不一致");
static_assert(sizeof(kMaterialFactories) / sizeof(kMaterialFactories[0]) ==
                  (size_t)kMaterialCount,
              "kMaterialFactories 与 kMaterialCatalog 数量不一致");

} // namespace

std::unique_ptr<IForm> createForm(const std::string& id) {
  for (int i = 0; i < kFormCount; ++i)
    if (id == kFormCatalog[i].id) return kFormFactories[i]();
  return nullptr;
}

std::unique_ptr<IMaterial> createMaterial(const std::string& id) {
  for (int i = 0; i < kMaterialCount; ++i)
    if (id == kMaterialCatalog[i].id) return kMaterialFactories[i]();
  return nullptr;
}

} // namespace okmeter::render
