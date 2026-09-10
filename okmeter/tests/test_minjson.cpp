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
