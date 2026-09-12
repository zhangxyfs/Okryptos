// core/fmt.h —— 数字紧凑/精确格式与相对时间（显示层口径，原型同款）
#pragma once
#include <cstdint>
#include <string>

namespace okmeter {

std::string fmtCompact(int64_t v);  // 9.9K / 128.6K / 2.1M；<1000 原样
std::string fmtExact(int64_t v);    // 千分位：17,029,868
std::string fmtYi(int64_t v);       // 万/亿单位（详情卡读数：9.9万 / 2.10亿）
std::string relTime(int64_t thenMs, int64_t nowMs);  // 刚刚 / N 分钟前 / N 小时前 / N 天前

} // namespace okmeter
