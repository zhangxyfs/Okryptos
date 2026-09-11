#include "../core/minjson.h"
#include "framework.h"

using namespace okmeter;

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

TEST(json_unicode_u_escape_bmp) {
  json::Value v;
  CHECK(json::parse(R"({"s":"\u4e2d"})", v));      // U+4E2D 中
  CHECK(v.find("s")->str() == "\xE4\xB8\xAD");                  // UTF-8 E4 B8 AD
  CHECK(json::parse(R"({"s":"x\u0000\uffffz"})", v));  // 边界码位
  CHECK(v.find("s")->str() == std::string("x\0\xEF\xBF\xBFz", 6));
}

TEST(json_unicode_u_escape_surrogate_pair) {
  json::Value v;
  CHECK(json::parse(R"({"s":"\ud83d\ude00"})", v));  // U+1F600 😀
  CHECK(v.find("s")->str() == "\xF0\x9F\x98\x80");            // UTF-8 F0 9F 98 80
}

TEST(json_unicode_u_escape_lone_surrogate_fallback) {
  // 现有实现语义：孤立代理不报错，按码位直接编 3 字节 UTF-8（WTF-8 风格）
  json::Value v;
  CHECK(json::parse(R"({"s":"\ud83d"})", v));            // 孤立高代理
  CHECK(v.find("s")->str() == "\xed\xa0\xbd");
  CHECK(json::parse(R"({"s":"\udc00"})", v));            // 孤立低代理
  CHECK(v.find("s")->str() == "\xed\xb0\x80");
  CHECK(json::parse(R"({"s":"\ud83dA"})", v));  // 高代理 + 非低代理：不组合
  CHECK(v.find("s")->str() == "\xed\xa0\xbd" "A");
}

TEST(json_unicode_u_escape_invalid_rejected) {
  json::Value v;
  CHECK(!json::parse(R"({"s":"\u4e2"})", v));   // 截断：hex4 读到引号
  CHECK(!json::parse(R"({"s":"\u12"})", v));    // 更短
  CHECK(!json::parse(R"({"s":"\uZZZZ"})", v));  // 非法 hex
  CHECK(!json::parse(R"({"s":"\u"})", v));      // 转义后无内容
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
