// adapters/kimi/adapter.h —— Kimi Code 适配器：sessions/**/wire.jsonl 游标增量扫描
#pragma once
#include "../adapter.h"
#include <filesystem>

namespace okmeter {

class Store;

class KimiAdapter : public IAdapter {
public:
  KimiAdapter(std::filesystem::path kimiHome, Store* store);
  std::string id() const override { return "kimi-code"; }
  int poll(const EventSink& sink) override;

private:
  int tailFile(const std::filesystem::path& f, const EventSink& sink);
  bool parseLine(std::string_view line, const std::string& sessionId,
                 const EventSink& sink);
  std::filesystem::path home_;
  Store* store_;
};

} // namespace okmeter
