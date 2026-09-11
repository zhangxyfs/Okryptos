#include "../render/catalog.h"
#include "framework.h"
#include <cstring>

using namespace okmeter;

TEST(catalog_forms) {
  CHECK_EQ(render::kFormCount, 3);
  CHECK(std::strcmp(render::kFormCatalog[0].id, "arc") == 0);
  CHECK(std::strcmp(render::kFormCatalog[1].id, "capsule") == 0);
  CHECK(std::strcmp(render::kFormCatalog[2].id, "compass") == 0);
  for (int i = 0; i < render::kFormCount; ++i) {
    CHECK(render::kFormCatalog[i].name != nullptr);
    CHECK(render::kFormCatalog[i].name[0] != L'\0');
    CHECK(render::kFormCatalog[i].sub == nullptr);   // 形态无副标题
    CHECK(render::kFormCatalog[i].thumb == i);       // 缩略图种类与序一致且互异
  }
  CHECK(render::findForm("arc") == &render::kFormCatalog[0]);
  CHECK(render::findForm("compass") == &render::kFormCatalog[2]);
  CHECK(render::findForm("nope") == nullptr);  // 未知 id → nullptr（调用方兜底）
  CHECK(render::findForm("") == nullptr);
}

TEST(catalog_materials) {
  CHECK_EQ(render::kMaterialCount, 4);
  CHECK(std::strcmp(render::kMaterialCatalog[0].id, "dark") == 0);
  CHECK(std::strcmp(render::kMaterialCatalog[1].id, "frost") == 0);
  CHECK(std::strcmp(render::kMaterialCatalog[2].id, "liquid") == 0);
  CHECK(std::strcmp(render::kMaterialCatalog[3].id, "glow") == 0);
  for (int i = 0; i < render::kMaterialCount; ++i) {
    CHECK(render::kMaterialCatalog[i].name != nullptr);
    CHECK(render::kMaterialCatalog[i].name[0] != L'\0');
    CHECK(render::kMaterialCatalog[i].sub != nullptr);
    CHECK(render::kMaterialCatalog[i].sub[0] != L'\0');  // 材质必有副标题
    CHECK(render::kMaterialCatalog[i].thumb == i);
  }
  CHECK(render::findMaterial("dark") == &render::kMaterialCatalog[0]);
  CHECK(render::findMaterial("glow") == &render::kMaterialCatalog[3]);
  CHECK(render::findMaterial("nope") == nullptr);
  CHECK(render::findMaterial("") == nullptr);
}
