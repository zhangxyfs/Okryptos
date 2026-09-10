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
