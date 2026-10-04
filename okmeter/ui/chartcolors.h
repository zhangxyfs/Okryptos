// ui/chartcolors.h —— 用量总览图表配色（拍板 9：数量不限、两两可辨；集中一处、
// 纯数据可单测，无 D2D 依赖）。规则同原型 prototype-overview-v1 assignColors：
//   · 策展色：已知大厂固定语义色（HSL 直定，两两 ΔHue≥25°——v1 教训：通义 28°
//     与智谱 45° 撞车，智谱用 90° 黄绿）；
//   · 溢出提供商：黄金角 137.5° HSL 步进（S=65% L=55% 固定），与策展色/已生成色
//     冲突（ΔHue<25°）再步进一位；一个完整周期（144 步）内无解时取色距最大者
//     （策展表稠密时严格规则不可满足，退化防死循环）；序 = 调用方给的规范序
//     （字典序，同会话稳定）；
//   · 下钻模型色：提供商色相 ±18° 小步旋转 + 明度交错（同族可辨）。
// 材质只决定底板/描边/文字色，图表语义色跨材质一致（不随材质换肤）。
#pragma once
#include <string>
#include <vector>

namespace okmeter::chartcolors {

struct Hsl { float h = 0, s = 0, l = 0; };  // h∈[0,360) s/l∈[0,100]
struct Rgb { float r = 0, g = 0, b = 0; };  // ∈[0,1]

float hueDist(float a, float b);            // 环上色距 ∈[0,180]
bool curated(const std::string& provider, Hsl& out);  // 命中策展色表
// 为整组提供商配色（与 names 对齐返回）：策展命中固定色；溢出按 names 内出现序
// 黄金角步进 + 避让（同序同色，调用方应传规范序如字典序保证跨帧稳定）。
std::vector<Hsl> providerColors(const std::vector<std::string>& names);
Hsl drilled(Hsl base, int i, int n);        // 下钻模型色：i/n = 模型序/总数
Rgb toRgb(Hsl c);

} // namespace okmeter::chartcolors
