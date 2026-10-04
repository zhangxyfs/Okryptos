#include "overview.h"
#include "../core/fmt.h"
#include "../core/provider.h"
#include <algorithm>
#include <cmath>
#include <cstdio>
#include <cwchar>

using Microsoft::WRL::ComPtr;

namespace okmeter {
namespace {

std::wstring wide(const std::string& s) {
  if (s.empty()) return {};
  const int n = MultiByteToWideChar(CP_UTF8, 0, s.c_str(), (int)s.size(), nullptr, 0);
  std::wstring out((size_t)n, L'\0');
  MultiByteToWideChar(CP_UTF8, 0, s.c_str(), (int)s.size(), out.data(), n);
  return out;
}

std::string shortName(const std::string& modelId) {
  const size_t p = modelId.rfind('/');
  return p == std::string::npos ? modelId : modelId.substr(p + 1);
}

std::string vendorOf(const std::string& modelId) {  // 命名空间段（/ 前）
  const size_t p = modelId.rfind('/');
  return p == std::string::npos ? std::string() : modelId.substr(0, p);
}

std::string lowerId(std::string s) {  // 大小写不敏感归并键（GLM-5.3 ≡ glm-5.3）
  for (auto& c : s)
    if (c >= 'A' && c <= 'Z') c = (char)(c - 'A' + 'a');
  return s;
}

D2D1_COLOR_F segColor(chartcolors::Hsl c, float a) {
  const chartcolors::Rgb r = chartcolors::toRgb(c);
  return D2D1::ColorF(r.r, r.g, r.b, a);
}

int64_t dayMs(int key) {  // dayKey → 当日正午 ms（避开 DST 边缘，bucket.cpp 同款）
  std::tm t{};
  t.tm_year = key / 10000 - 1900;
  t.tm_mon = (key / 100) % 100 - 1;
  t.tm_mday = key % 100;
  t.tm_hour = 12;
  return (int64_t)std::mktime(&t) * 1000;
}

// Fritsch–Carlson 单调三次切线（原型 curveEval 同款；过点不超调）
std::vector<float> fcTangents(const std::vector<D2D1_POINT_2F>& p) {
  const size_t n = p.size();
  std::vector<float> m(n, 0.0f);
  if (n < 2) return m;
  std::vector<float> d(n - 1, 0.0f);
  for (size_t i = 0; i + 1 < n; ++i) {
    const float dx = p[i + 1].x - p[i].x;
    d[i] = dx > 1e-9f ? (p[i + 1].y - p[i].y) / dx : 0.0f;
  }
  m[0] = d[0];
  for (size_t i = 1; i + 1 < n; ++i)
    m[i] = d[i - 1] * d[i] <= 0 ? 0.0f : (d[i - 1] + d[i]) * 0.5f;
  m[n - 1] = d[n - 2];
  for (size_t i = 0; i + 1 < n; ++i) {
    if (d[i] == 0.0f) { m[i] = 0.0f; m[i + 1] = 0.0f; continue; }
    const float a = m[i] / d[i], b = m[i + 1] / d[i];
    const float q = a * a + b * b;
    if (q > 9.0f) {
      const float t = 3.0f / std::sqrt(q);
      m[i] = t * a * d[i];
      m[i + 1] = t * b * d[i];
    }
  }
  return m;
}

// FC 连续取值器（趋势 morph 采样用：x 像素域、y 值域；端点外按端点值水平外延）
struct FcEval {
  std::vector<D2D1_POINT_2F> p;
  std::vector<float> m;
  double operator()(float x) const {
    const size_t n = p.size();
    if (n == 0) return 0;
    if (n == 1 || x <= p[0].x) return p[0].y;
    if (x >= p[n - 1].x) return p[n - 1].y;
    size_t lo = 0, hi = n - 1;
    while (hi - lo > 1) {
      const size_t mid = (lo + hi) >> 1;
      if (p[mid].x <= x) lo = mid;
      else hi = mid;
    }
    const float h = p[hi].x - p[lo].x;
    const float t = (x - p[lo].x) / h;
    const float t2 = t * t, t3 = t2 * t;
    return (2 * t3 - 3 * t2 + 1) * p[lo].y + (t3 - 2 * t2 + t) * h * m[lo] +
           (-2 * t3 + 3 * t2) * p[hi].y + (t3 - 2 * t2) * h * m[hi];
  }
};

FcEval fcCurve(const std::vector<D2D1_POINT_2F>& pts) {
  return FcEval{pts, fcTangents(pts)};
}

// 原型 --ease-dock：cubic-bezier(.22,.8,.3,1)；对参数 t 牛顿迭代求 x 后的 y 值
//（app.cpp 同款——morph 缓动与 dock 弹簧/菜单/面板出现动画一致）
double easeDock(double x) {
  if (x <= 0) return 0;
  if (x >= 1) return 1;
  double t = x;
  for (int i = 0; i < 5; ++i) {
    const double u = 1 - t;
    const double cx = 3*u*u*t*0.22 + 3*u*t*t*0.30 + t*t*t - x;
    const double dx = 3*u*u*0.22 + 6*u*t*(0.30 - 0.22) + 3*t*t*(1.0 - 0.30);
    if (std::abs(dx) < 1e-9) break;
    t -= cx / dx;
    t = t < 0 ? 0 : (t > 1 ? 1 : t);
  }
  const double u = 1 - t;
  return 3*u*u*t*0.8 + 3*u*t*t*1.0 + t*t*t;
}

// 图表 morph 专用 ease-in-out cubic：两端都缓。ease-dock 是重度 ease-out
//（首帧即 ~20% 进度），柱/曲线的长度形变会被读成"瞬间变长/变短"
double easeInOut(double x) {
  if (x <= 0) return 0;
  if (x >= 1) return 1;
  return x < 0.5 ? 4.0 * x * x * x : 1.0 - std::pow(-2.0 * x + 2.0, 3.0) / 2.0;
}

} // namespace

bool OverviewPanel::ensure(render::D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  if (!dc || !d3d.dwrite()) return false;
  if (dc == seen_ && seenGen_ == d3d.generation() && brush_ && dashStyle_ &&
      titleFmt_ && enFmt_ && tabFmt_ && bodyFmt_ && smallFmt_ && monoFmt_ &&
      tipFmt_ && footFmt_ && numFmt_)
    return true;
  seen_ = dc;
  seenGen_ = d3d.generation();
  brush_.Reset();
  dashStyle_.Reset();
  grads_.clear();
  titleFmt_.Reset(); enFmt_.Reset(); tabFmt_.Reset(); bodyFmt_.Reset();
  smallFmt_.Reset(); monoFmt_.Reset(); tipFmt_.Reset(); footFmt_.Reset();
  numFmt_.Reset();
  if (FAILED(dc->CreateSolidColorBrush(D2D1::ColorF(0, 0), &brush_))) return false;
  ComPtr<ID2D1Factory> f;
  dc->GetFactory(&f);
  if (f) {
    const float dashes[] = {3.0f, 3.0f};
    D2D1_STROKE_STYLE_PROPERTIES sp{};
    sp.dashStyle = D2D1_DASH_STYLE_CUSTOM;
    (void)f->CreateStrokeStyle(&sp, dashes, 2, &dashStyle_);
  }
  if (!dashStyle_) return false;
  IDWriteFactory* dw = d3d.dwrite();
  auto make = [&](float size, DWRITE_FONT_WEIGHT weight,
                  DWRITE_TEXT_ALIGNMENT halign, IDWriteTextFormat** out) {
    if (FAILED(dw->CreateTextFormat(L"Segoe UI", nullptr, weight,
                                    DWRITE_FONT_STYLE_NORMAL, DWRITE_FONT_STRETCH_NORMAL,
                                    size, L"", out)))
      return false;
    (*out)->SetTextAlignment(halign);
    (*out)->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
    return true;
  };
  const bool ok =
      make(13.0f, DWRITE_FONT_WEIGHT_SEMI_BOLD, DWRITE_TEXT_ALIGNMENT_LEADING,
           &titleFmt_) &&
      make(9.0f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_TRAILING,
           &enFmt_) &&
      make(11.5f, DWRITE_FONT_WEIGHT_MEDIUM, DWRITE_TEXT_ALIGNMENT_CENTER,
           &tabFmt_) &&
      make(12.5f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_LEADING,
           &bodyFmt_) &&
      make(10.5f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_LEADING,
           &smallFmt_) &&
      make(11.5f, DWRITE_FONT_WEIGHT_SEMI_BOLD, DWRITE_TEXT_ALIGNMENT_LEADING,
           &tipFmt_) &&
      make(10.5f, DWRITE_FONT_WEIGHT_NORMAL, DWRITE_TEXT_ALIGNMENT_LEADING,
           &footFmt_) &&
      [&] {
        if (FAILED(dw->CreateTextFormat(L"Consolas", nullptr,
                                        DWRITE_FONT_WEIGHT_NORMAL,
                                        DWRITE_FONT_STYLE_NORMAL,
                                        DWRITE_FONT_STRETCH_NORMAL, 10.5f, L"",
                                        &monoFmt_)))
          return false;
        monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
        monoFmt_->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
        return true;
      }() &&
      [&] {  // 统计卡大数 Consolas 15 中等
        if (FAILED(dw->CreateTextFormat(L"Consolas", nullptr,
                                        DWRITE_FONT_WEIGHT_MEDIUM,
                                        DWRITE_FONT_STYLE_NORMAL,
                                        DWRITE_FONT_STRETCH_NORMAL, 15.0f, L"",
                                        &numFmt_)))
          return false;
        numFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
        numFmt_->SetParagraphAlignment(DWRITE_PARAGRAPH_ALIGNMENT_CENTER);
        return true;
      }();
  return ok;
}

float OverviewPanel::measure(IDWriteTextFormat* fmt, const std::wstring& s) const {
  // 兜底估值（layout/drawTooltip 的 mwidth 在 dwrite 不可用时走这里）：
  // 粗体/Consolas 差异不敏感，仅用于控件宽度，误差可接受
  (void)fmt;
  float w = 0;
  for (wchar_t ch : s) w += ch < 128 ? 6.5f : 11.0f;
  return w;
}

int64_t OverviewPanel::niceStep(int64_t raw) {
  if (raw <= 0) return 1;
  const double p = std::pow(10.0, std::floor(std::log10((double)raw)));
  const double f = (double)raw / p;
  const double n = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10;
  return (int64_t)(n * p);
}

int OverviewPanel::addDaysK(int key, int n) {
  return Aggregator::dayKey(dayMs(key) + (int64_t)n * 86400000);
}

int OverviewPanel::heatLevel(int64_t v) const {
  // 5 档（0/低/中/高/极高），阈值 = 当前粒度窗口内非零格分位（全零窗口全 0 档）
  if (v <= 0) return 0;
  if (v <= heat_.p50) return 1;
  if (v <= heat_.p75) return 2;
  if (v <= heat_.p90) return 3;
  return 4;
}

const std::set<std::string>& OverviewPanel::hiddenSet() const {
  static const std::set<std::string> kEmpty;
  if (drill_.empty()) return hiddenProv_;
  auto it = hiddenModels_.find(drill_);
  return it == hiddenModels_.end() ? kEmpty : it->second;
}

std::set<std::string>& OverviewPanel::hiddenSet() {
  return drill_.empty() ? hiddenProv_ : hiddenModels_[drill_];
}

int OverviewPanel::visibleCount() const {
  int n = 0;
  for (const Seg& s : segs_)
    if (!s.hidden) ++n;
  return n;
}

void OverviewPanel::setTheme(const std::string& theme) {
  // 主题 token 照 prototype-overview-v2 两套 CSS 变量（:root 浅色 / [data-theme='dark']
  // 深色，oklch→sRGB 已换算）；图表语义色 chartcolors 不变，仅界面图层翻转
  theme_ = theme == "dark" ? "dark" : "light";
  if (theme_ == "dark") {
    th_.bg = D2D1::ColorF(0.0792f, 0.1016f, 0.1306f, 1.0f);      // --surface oklch(21.5%)
    th_.surface = D2D1::ColorF(0.0792f, 0.1016f, 0.1306f, 1.0f);  // 全不透明：不透桌面
    th_.fg = D2D1::ColorF(0.8938f, 0.9111f, 0.9297f, 1.0f);      // --fg oklch(93%)
    th_.muted = D2D1::ColorF(0.5623f, 0.5883f, 0.6161f, 1.0f);   // --muted oklch(67%)
    th_.border = D2D1::ColorF(0.8938f, 0.9111f, 0.9297f, 0.15f); // --border = fg 15%
    th_.accent = D2D1::ColorF(0.3725f, 0.6533f, 1.0f, 1.0f);     // --accent oklch(72% .15 255)
    th_.heatA[0] = 0.30f; th_.heatA[1] = 0.50f;
    th_.heatA[2] = 0.74f; th_.heatA[3] = 1.0f;
    th_.heat0 = 0.07f;                                           // --heat0 = fg 7%
  } else {
    th_.bg = D2D1::ColorF(1.0f, 1.0f, 1.0f, 1.0f);               // --surface oklch(100%)
    th_.surface = D2D1::ColorF(1.0f, 1.0f, 1.0f, 1.0f);          // 全不透明：不透桌面
    th_.fg = D2D1::ColorF(0.0534f, 0.0708f, 0.0893f, 1.0f);      // --fg oklch(18%)
    th_.muted = D2D1::ColorF(0.4139f, 0.4366f, 0.4610f, 1.0f);   // --muted oklch(54%)
    th_.border = D2D1::ColorF(0.8864f, 0.8972f, 0.9088f, 1.0f);  // --border oklch(92%)
    th_.accent = D2D1::ColorF(0.0892f, 0.4726f, 0.8827f, 1.0f);  // --accent oklch(58% .18 255)
    th_.heatA[0] = 0.24f; th_.heatA[1] = 0.42f;
    th_.heatA[2] = 0.66f; th_.heatA[3] = 1.0f;
    th_.heat0 = 0.06f;                                           // --heat0 = fg 6%
  }
}

D2D1_COLOR_F OverviewPanel::thInk(float a) const {
  D2D1_COLOR_F c = th().fg;
  c.a = a;
  return c;
}

D2D1_COLOR_F OverviewPanel::thMuted(float a) const {
  D2D1_COLOR_F c = th().muted;
  c.a = a;
  return c;
}

D2D1_COLOR_F OverviewPanel::thAccent(float a) const {
  D2D1_COLOR_F c = th().accent;
  c.a = a;
  return c;
}

void OverviewPanel::begin(const Aggregator& agg) {
  open = true;
  hover = -1;
  setTheme(theme_);  // 防从未注入时零值 token（app 每次 open 也会按 config 注入）
  hoverBucket_ = -1;
  dimSeg_.clear();
  // 视图状态不写盘，每次打开默认「日 · 柱状 · 每日活动图」（关窗即忘）
  range_ = BucketRange::Day;
  kind_ = ChartKind::Bars;
  act_ = ActGrain::Day;
  drill_.clear();
  hiddenProv_.clear();
  hiddenModels_.clear();
  scrollTable_ = scrollLegend_ = 0;
  wheelResTable_ = wheelResLegend_ = 0;
  segs_.clear();
  buckets_.clear();
  trows_.clear();
  heat_ = Heatmap{};
  sum_ = TotalsSummary{};
  hoverCell_ = -1;
  maxV_ = 0;
  panOffset_ = 0;   // 平移/缩放/动画状态复位（视图状态关窗即忘）
  maxPan_ = 0;
  bwCustom_ = false;
  panArmed_ = panning_ = false;
  animType_ = pendingAnim_ = 0;
  animCur_ = PlotFrame{};
  animE_ = 1.0;
  geomDirty_ = true;
  if (agg.days().empty()) {
    foot_ = L"暂无数据 · token=输入+输出（含缓存）";
  } else {
    const int k = agg.days().begin()->first;  // 最早 byDay（std::map 升序）
    wchar_t buf[16];
    swprintf_s(buf, L"%04d-%02d-%02d", k / 10000, (k / 100) % 100, k % 100);
    foot_ = L"数据自 " + std::wstring(buf) + L" 起 · token=输入+输出（含缓存）";
  }
}

void OverviewPanel::refresh(const Aggregator& agg, int64_t nowMs) {
  // 桶数固定（拍板 10）：日 7 / 周 4 / 月 12 / 年 = 数据实际跨度
  int count = 7;
  if (range_ == BucketRange::Week) count = 4;
  else if (range_ == BucketRange::Month) count = 12;
  else if (range_ == BucketRange::Year) {
    const int curY = Aggregator::dayKey(nowMs) / 10000;
    const int firstY = agg.days().empty() ? curY : agg.days().begin()->first / 10000;
    count = (std::max)(1, curY - firstY + 1);
  }
  rangeCount_ = count;
  // 数据起点→当前桶总桶数（平移夹取上限；无数据 = 固定桶数）
  todayKey_ = Aggregator::dayKey(nowMs);
  if (agg.days().empty()) {
    totalBuckets_ = count;
  } else {
    const int first = agg.days().begin()->first;
    const int64_t spanD = (dayMs(todayKey_) - dayMs(first)) / 86400000;
    if (range_ == BucketRange::Day) totalBuckets_ = (int)spanD + 1;
    else if (range_ == BucketRange::Week) {
      const int64_t wspan = (dayMs(Aggregator::weekStartKey(dayMs(todayKey_))) -
                             dayMs(Aggregator::weekStartKey(dayMs(first)))) / 86400000;
      totalBuckets_ = (int)(wspan / 7) + 1;
    } else if (range_ == BucketRange::Month) {
      totalBuckets_ = (todayKey_ / 10000 * 12 + (todayKey_ / 100) % 100) -
                      (first / 10000 * 12 + (first / 100) % 100) + 1;
    } else {
      totalBuckets_ = todayKey_ / 10000 - first / 10000 + 1;
    }
  }
  // 平移数据窗：整数基 = floor(panOffset_)，浮点余量由绘制侧滑移；取窗 =
  // 可见桶数 + 2 桶余量（几何未就绪的首刷退化为 固定桶数+2），裁剪不越界
  const int base = (int)std::floor(panOffset_);
  int need = count + 2;
  if (!geomDirty_ && bw_ > 0.0f && plotW_ > 0.0f)
    need = (std::min)(need, (int)std::ceil(plotW_ / bw_) + 2);
  need = (std::min)(need, (std::max)(totalBuckets_ - base, 1));
  const std::vector<Bucket> series = bucketSeries(agg, range_, need, base, nowMs, drill_);
  buckets_.assign(series.size(), BucketF{});
  std::map<std::string, int64_t> totals;
  std::map<std::string, std::string> labels;
  for (size_t i = 0; i < series.size(); ++i) {
    buckets_[i].key = series[i].key;
    buckets_[i].beginKey = series[i].beginKey;
    buckets_[i].endKey = series[i].endKey;
    for (const Segment& s : series[i].segments) {
      totals[s.id] += s.sums.total();
      labels[s.id] = s.label;
    }
  }
  if (range_ == BucketRange::Week && !buckets_.empty())  // 当周 tooltip 止于今天
    buckets_.back().endKey = (std::min)(buckets_.back().endKey,
                                        Aggregator::dayKey(nowMs));
  // 候选分段：窗口内有量的 + 隐藏集合中的（隐藏项保持列出，原型同款）
  const std::set<std::string>& hidden = hiddenSet();
  std::vector<std::string> ids;
  auto consider = [&](const std::string& id) {
    if ((totals[id] > 0 || hidden.count(id)) &&
        std::find(ids.begin(), ids.end(), id) == ids.end())
      ids.push_back(id);
  };
  for (const std::string& m : agg.modelsByRecency()) {
    if (drill_.empty()) consider(providerOf(m));
    else if (providerOf(m) == drill_) consider(lowerId(m));  // 下钻按小写归并
  }
  for (const auto& [id, t] : totals) {
    (void)t;
    consider(id);
  }
  // 排序：可见按窗口合计降序，隐藏垫后
  std::stable_sort(ids.begin(), ids.end(), [&](const std::string& x, const std::string& y) {
    const bool hx = hidden.count(x) > 0, hy = hidden.count(y) > 0;
    if (hx != hy) return !hx;
    return totals[x] > totals[y];
  });
  // 配色：提供商层 = 策展/黄金角（全部提供商按字典序给序，跨帧稳定）；
  // 下钻层 = 提供商基色 ±18° 旋转 + 明度交错（模型按字典序给色序，同色稳定）
  std::vector<std::string> allProv;
  for (const std::string& m : agg.modelsByRecency()) {
    const std::string p = providerOf(m);
    if (std::find(allProv.begin(), allProv.end(), p) == allProv.end())
      allProv.push_back(p);
  }
  std::sort(allProv.begin(), allProv.end());
  const std::vector<chartcolors::Hsl> provCols = chartcolors::providerColors(allProv);
  std::map<std::string, chartcolors::Hsl> colorOf;
  chartcolors::Hsl drillBase{200.0f, 65.0f, 55.0f};
  for (size_t i = 0; i < allProv.size(); ++i) {
    if (drill_.empty()) colorOf[allProv[i]] = provCols[i];
    if (allProv[i] == drill_) drillBase = provCols[i];
  }
  if (!drill_.empty()) {  // 色序同样按归并后的小写 id（与 segments 键一致）
    std::vector<std::string> ms;
    for (const std::string& m : agg.modelsByRecency())
      if (providerOf(m) == drill_) {
        const std::string lw = lowerId(m);
        if (std::find(ms.begin(), ms.end(), lw) == ms.end()) ms.push_back(lw);
      }
    std::sort(ms.begin(), ms.end());
    for (size_t i = 0; i < ms.size(); ++i)
      colorOf[ms[i]] = chartcolors::drilled(drillBase, (int)i, (int)ms.size());
  }
  segs_.clear();
  for (const std::string& id : ids) {
    Seg s;
    s.id = id;
    s.label = labels.count(id) ? labels[id] : shortName(id);
    s.hsl = colorOf[id];
    s.windowTotal = totals[id];
    s.hidden = hidden.count(id) > 0;
    segs_.push_back(std::move(s));
  }
  // 每桶分段值对齐 segs_
  std::map<std::string, size_t> segIdx;
  for (size_t i = 0; i < segs_.size(); ++i) segIdx[segs_[i].id] = i;
  for (size_t i = 0; i < series.size(); ++i) {
    buckets_[i].segVals.assign(segs_.size(), 0);
    for (const Segment& s : series[i].segments) {
      auto it = segIdx.find(s.id);
      if (it != segIdx.end()) buckets_[i].segVals[it->second] = s.sums.total();
    }
  }
  // 可见极值（柱 = 可见合计最大；趋势 = 单段最大）
  maxV_ = 0;
  for (const BucketF& b : buckets_) {
    if (kind_ == ChartKind::Bars) {
      int64_t sum = 0;
      for (size_t j = 0; j < segs_.size(); ++j)
        if (!segs_[j].hidden) sum += b.segVals[j];
      if (sum > maxV_) maxV_ = sum;
    } else {
      for (size_t j = 0; j < segs_.size(); ++j)
        if (!segs_[j].hidden && b.segVals[j] > maxV_) maxV_ = b.segVals[j];
    }
  }
  // ── 明细表（隐藏分段不列；提供商层两级降序，下钻只列该商模型）──
  trows_.clear();
  const int firstDay = buckets_.empty() ? 0 : buckets_.front().beginKey;
  const int lastDay = buckets_.empty() ? 0 : buckets_.back().endKey;
  auto modelSum = [&](const std::string& id) {
    const ModelStat* m = agg.model(id);
    int64_t t = 0;
    if (m)
      for (const auto& [k, s] : m->byDay)
        if (k >= firstDay && k <= lastDay) t += s.total();
    return t;
  };
  if (drill_.empty()) {
    for (const Seg& sg : segs_) {
      if (sg.hidden) continue;
      TRow g;
      g.group = true;
      g.segId = sg.id;
      g.name = wide(sg.label);
      g.val = sg.windowTotal;
      g.hsl = sg.hsl;
      trows_.push_back(std::move(g));
      // 模型行：小写归并（GLM-5.3 ≡ glm-5.3），值=变体求和，显示名=最近使用的
      // 原始大小写（modelsByRecency 降序，首个即最近）；归并后仍同短名的
      //（kimi-code/k3 vs claude-code/k3）加" · 命名空间"（级联菜单既有规则）
      struct MRow { std::string lower, best; int64_t val = 0; int ci = 0; };
      std::vector<MRow> mrows;
      for (const std::string& m : agg.modelsByRecency()) {
        if (providerOf(m) != sg.id) continue;
        const std::string lw = lowerId(m);
        bool found = false;
        for (MRow& r : mrows)
          if (r.lower == lw) { r.val += modelSum(m); found = true; break; }
        if (!found) mrows.push_back(MRow{lw, m, modelSum(m), 0});
      }
      {
        std::vector<std::string> sorted;
        for (const MRow& r : mrows) sorted.push_back(r.lower);
        std::sort(sorted.begin(), sorted.end());  // 色序（lower 字典序，稳定）
        for (MRow& r : mrows)
          r.ci = (int)(std::find(sorted.begin(), sorted.end(), r.lower) - sorted.begin());
      }
      std::stable_sort(mrows.begin(), mrows.end(),  // 展示序（合计降序）
                       [](const MRow& a, const MRow& b) { return a.val > b.val; });
      for (const MRow& r : mrows) {
        int dup = 0;
        for (const MRow& o : mrows)
          if (lowerId(shortName(o.best)) == lowerId(shortName(r.best))) ++dup;
        TRow row;
        row.segId = sg.id;  // 模型行 dim 其所属提供商分段（需求 · 明细表）
        row.name = wide(shortName(r.best) + (dup > 1 ? " · " + vendorOf(r.best) : ""));
        row.val = r.val;
        row.hsl = chartcolors::drilled(sg.hsl, r.ci, (int)mrows.size());
        trows_.push_back(std::move(row));
      }
    }
  } else {
    TRow g;
    g.group = true;
    g.name = wide(drill_);
    g.hsl = drillBase;
    for (const Seg& sg : segs_)
      if (!sg.hidden) g.val += sg.windowTotal;
    trows_.push_back(std::move(g));  // segId 空：组头无 dim 目标
    for (const Seg& sg : segs_) {
      if (sg.hidden) continue;
      TRow r;
      r.segId = sg.id;
      r.name = wide(sg.label);
      r.val = sg.windowTotal;
      r.hsl = sg.hsl;
      trows_.push_back(std::move(r));
    }
  }
  // ── Token 活动热力图 + 累计摘要（随 poll 重算；摘要不随粒度 tab 联动）──
  const HeatGrain hg = act_ == ActGrain::Day ? HeatGrain::Day
                     : act_ == ActGrain::Week ? HeatGrain::Week
                                              : HeatGrain::WeekCumulative;
  heat_ = heatmap(agg, hg, 52, nowMs);
  sum_ = totalsSummary(agg, nowMs);
  geomDirty_ = true;
}

void OverviewPanel::layout(render::D3DContext& d3d) {
  ctrls_.clear();
  hover = -1;
  dimSeg_.clear();
  const float W = rect.right - rect.left;
  const float H = rect.bottom - rect.top;
  ctrls_.push_back(Ctrl{Ctrl::CloseX, 0, D2D1::RectF(W - 37.0f, 12.0f, W - 15.0f, 34.0f)});
  ctrls_.push_back(Ctrl{Ctrl::ThemeBtn, 0,
                        D2D1::RectF(W - 37.0f - 30.0f, 12.0f, W - 37.0f - 8.0f, 34.0f)});
  ctrls_.push_back(Ctrl{Ctrl::ShareBtn, 0,
                        D2D1::RectF(W - 37.0f - 60.0f, 12.0f, W - 37.0f - 38.0f, 34.0f)});
  ctrls_.push_back(Ctrl{Ctrl::PinBtn, 0,
                        D2D1::RectF(W - 37.0f - 90.0f, 12.0f, W - 37.0f - 68.0f, 34.0f)});
  const bool hasDev = ensure(d3d);
  IDWriteFactory* dw = hasDev ? d3d.dwrite() : nullptr;
  auto mwidth = [&](IDWriteTextFormat* fmt, const std::wstring& s) {
    if (dw && fmt) {
      ComPtr<IDWriteTextLayout> tl;
      if (SUCCEEDED(dw->CreateTextLayout(s.c_str(), (UINT32)s.size(), fmt,
                                         2000.0f, 100.0f, &tl))) {
        DWRITE_TEXT_METRICS m{};
        if (SUCCEEDED(tl->GetMetrics(&m))) return m.width;
      }
    }
    return measure(fmt, s);
  };

  // ── 顶栏：面包屑（下钻时）+ 范围 tabs（左）+ 图型分段钮（右）──
  const float tbY = kHdH + (kTbH - 26.0f) * 0.5f;
  float tx = 15.0f;
  if (!drill_.empty()) {
    const float cw = mwidth(tabFmt_.Get(), L"‹ 返回提供商") + 6.0f;
    ctrls_.push_back(Ctrl{Ctrl::Crumb, 0, D2D1::RectF(tx, tbY, tx + cw, tbY + 26.0f)});
    tx += cw + 6.0f + mwidth(smallFmt_.Get(), L"/ " + wide(drill_)) + 14.0f;
  }
  for (int i = 0; i < 4; ++i)  // 范围 tabs（等宽 40）
    ctrls_.push_back(Ctrl{Ctrl::RangeTab, i,
                          D2D1::RectF(tx + 40.0f * i, tbY, tx + 40.0f * (i + 1), tbY + 26.0f)});
  const float kx = W - 15.0f - 104.0f;  // 图型分段钮（2 × 52）
  for (int i = 0; i < 2; ++i)
    ctrls_.push_back(Ctrl{Ctrl::KindBtn, i,
                          D2D1::RectF(kx + 52.0f * i, tbY, kx + 52.0f * (i + 1), tbY + 26.0f)});
  // ── 区域：主图 : 右栏 ≈ 7:3；主图区 : 活动图区 ≈ 3:2；右栏图例 : 明细 ≈ 2:3 ──
  const float top = kHdH + kTbH + 6.0f;
  const float bot = H - kFtH - 8.0f;
  const float mainH = (bot - top - kGap) * 0.6f;
  const float mainW = (W - 2.0f * kPad - kGap) * 0.7f;
  mainRc_ = D2D1::RectF(kPad, top, kPad + mainW, top + mainH);
  const float railX0 = mainRc_.right + kGap;
  const float legendH = (mainH - kGap) * 0.4f;
  legendRc_ = D2D1::RectF(railX0, top, W - kPad, top + legendH);
  tableRc_ = D2D1::RectF(railX0, legendRc_.bottom + kGap, W - kPad, top + mainH);
  actRc_ = D2D1::RectF(kPad, top + mainH + kGap, W - kPad, bot);

  // ── 活动图区：cap 行 + 格阵（左）+ 统计卡组（右，约 36% 宽，竖线分隔）──
  const float actW = actRc_.right - actRc_.left;
  // 统计卡列最小 208px（不够时优先压缩格阵 cell 下限，卡组必须完整可见）
  const float statsW = (std::max)(208.0f, (std::min)(340.0f, actW * 0.36f));
  gridRc_ = D2D1::RectF(actRc_.left, actRc_.top, actRc_.right - statsW - 14.0f,
                        actRc_.bottom);
  statsRc_ = D2D1::RectF(gridRc_.right + 14.0f, actRc_.top, actRc_.right, actRc_.bottom);
  {  // 粒度 tabs [每日|每周|累计]：格阵区右上 cap 行内（面板坐标控件，须在图例控件前入表）
    const float atX = gridRc_.right - 8.0f - 3.0f * 46.0f;
    for (int i = 0; i < 3; ++i)
      ctrls_.push_back(Ctrl{Ctrl::ActTab, i,
                            D2D1::RectF(atX + 46.0f * i, actRc_.top + 3.0f,
                                        atX + 46.0f * (i + 1), actRc_.top + 21.0f)});
  }
  {  // 格阵几何：GitHub 式 7 行 × N 周列（三档统一）；近正方形按区域宽度现算
    // 铺满（clamp 7–20），高度受限时居中；左侧留星期行标条（一/三/五/日）
    rows_ = 7;
    const size_t nc = heat_.cells.size();
    cols_ = act_ == ActGrain::Week ? (int)nc : (int)((nc + 6) / 7);
    if (cols_ < 1) cols_ = 1;
    constexpr float padL = 20.0f, gap = 2.0f;
    const float availW = (gridRc_.right - gridRc_.left) - padL - 8.0f;
    const float availH = (gridRc_.bottom - gridRc_.top) - 24.0f - 16.0f;  // cap 行 + 月份轴
    const float fitW = (availW - (float)(cols_ - 1) * gap) / (float)cols_;
    const float fitH = (availH - (float)(rows_ - 1) * gap) / (float)rows_;
    cell_ = (std::max)(5.0f, (std::min)(20.0f, std::floor((std::min)(fitW, fitH))));  // 下限 5px：统计卡列宽度优先
    gridX0_ = gridRc_.left + padL;
    gridY0_ = gridRc_.top + 24.0f +
              (std::max)(0.0f, (availH - (float)rows_ * (cell_ + gap)) * 0.5f);
  }
  {  // 统计卡 2 列 × 3 行；窄面板退化 1 列 × 6 行
    statOneCol_ = statsW < 200.0f;
    const float cardsTop = actRc_.top + 24.0f;
    const float cardsH = actRc_.bottom - cardsTop - 8.0f;
    if (!statOneCol_) {
      const float cw = (statsW - 3.0f * 8.0f) * 0.5f;
      const float rh = (cardsH - 2.0f * 8.0f) / 3.0f;
      for (int i = 0; i < 6; ++i) {
        const float cx = statsRc_.left + 8.0f + (float)(i % 2) * (cw + 8.0f);
        const float cy = cardsTop + (float)(i / 2) * (rh + 8.0f);
        statRc_[i] = D2D1::RectF(cx, cy, cx + cw, cy + rh);
      }
    } else {
      const float rh = (cardsH - 5.0f * 6.0f) / 6.0f;
      for (int i = 0; i < 6; ++i) {
        const float cy = cardsTop + (float)i * (rh + 6.0f);
        statRc_[i] = D2D1::RectF(statsRc_.left + 8.0f, cy, statsRc_.right - 8.0f, cy + rh);
      }
    }
    for (int i = 0; i < 6; ++i)
      ctrls_.push_back(Ctrl{Ctrl::StatCard, i, statRc_[i]});
  }

  // ── plot 区：Y 轴刻度宽实测（yMax 千分位标签）──
  if (maxV_ <= 0) {
    yMax_ = 1;
    gridStep_ = 1;
  } else {
    gridStep_ = niceStep((std::max)(maxV_ / 4, (int64_t)1));
    yMax_ = gridStep_ * ((maxV_ + gridStep_ - 1) / gridStep_);
  }
  const float yLabelW = mwidth(monoFmt_.Get(), wide(fmtExact(yMax_))) + 16.0f;
  plotX0_ = mainRc_.left + yLabelW;
  plotY0_ = mainRc_.top + 8.0f;
  plotW_ = (std::max)(40.0f, mainRc_.right - 8.0f - plotX0_);
  plotH_ = (std::max)(40.0f, mainRc_.bottom - 22.0f - plotY0_);
  // 单桶宽：自适应 = 固定桶数恰好容纳 plot（缩放/平移的下限基准）；
  // 用户缩放（bwCustom_）保持，范围切换/关窗复位
  const float fitBw = plotW_ / (float)(std::max)(1, rangeCount_);
  if (!bwCustom_) bw_ = fitBw;
  if (bw_ < fitBw) bw_ = fitBw;
  if (bw_ > 120.0f) bw_ = 120.0f;
  barW_ = (std::max)(2.0f, (std::min)((std::min)(bw_ * 0.52f, 34.0f), bw_ - 2.0f));
  maxPan_ = (std::max)(0.0, (double)totalBuckets_ - (double)plotW_ / (double)bw_);
  clampPan();

  // ── 图例控件（内容坐标；cap 占 22）──
  const float legendCW = (legendRc_.right - legendRc_.left) - 16.0f - 10.0f;
  for (size_t i = 0; i < segs_.size(); ++i) {
    const float y = 22.0f + (float)i * kLegendRowH;
    ctrls_.push_back(Ctrl{Ctrl::LegendToggle, (int)i,
                          D2D1::RectF(2.0f, y + 2.0f, 20.0f, y + kLegendRowH - 2.0f)});
    if (drill_.empty())
      ctrls_.push_back(Ctrl{Ctrl::LegendDrill, (int)i,
                            D2D1::RectF(22.0f, y, legendCW, y + kLegendRowH)});
  }
  legendContentH_ = 22 + (int)(segs_.size() * kLegendRowH);
  // ── 明细表行（内容坐标；cap 占 22）──
  const float tableCW = (tableRc_.right - tableRc_.left) - 16.0f - 10.0f;
  float ty = 22.0f;
  for (size_t i = 0; i < trows_.size(); ++i) {
    const float h = trows_[i].group ? kGroupRowH : kModelRowH;
    ctrls_.push_back(Ctrl{Ctrl::TableRow, (int)i, D2D1::RectF(0.0f, ty, tableCW, ty + h)});
    ty += h;
  }
  tableContentH_ = (int)ty;
  scrollLegend_ = (std::min)(scrollLegend_,
      (std::max)(0, legendContentH_ - (int)(legendRc_.bottom - legendRc_.top)));
  scrollTable_ = (std::min)(scrollTable_,
      (std::max)(0, tableContentH_ - (int)(tableRc_.bottom - tableRc_.top)));
  geomDirty_ = false;
}

void OverviewPanel::place(float x, float y, float w, float h) {
  const float oldW = rect.right - rect.left, oldH = rect.bottom - rect.top;
  rect = D2D1::RectF(x, y, x + w, y + h);
  if (std::fabs(w - oldW) > 0.5f || std::fabs(h - oldH) > 0.5f) geomDirty_ = true;
}

double OverviewPanel::plotYOf(double yMax, double v) const {
  return plotY0_ + plotH_ - (float)(v / yMax) * plotH_;
}

float OverviewPanel::frameBucketX(size_t i) const {
  // 连续坐标：panOffset_ 整数部换数据窗（refresh 的 base），小数部在此滑移——
  // 向右拖（panOffset_ 增大）内容右移，旧桶从左侧进入（原型 bucketX 同款）
  const double frac = panOffset_ - std::floor(panOffset_);
  return (float)(plotX0_ + plotW_ - ((double)(buckets_.size() - 1 - i) + 0.5 - frac) * bw_);
}

void OverviewPanel::clampPan() {
  panOffset_ = (std::max)(0.0, (std::min)(panOffset_, maxPan_));
}

std::wstring OverviewPanel::tickLabelForKey(int key, BucketRange range) const {
  // 轴刻度：日/周 = M/D；月 = M月（1 月带年）；年 = Y（与柱中心共用 x 天然对齐）
  wchar_t buf[24];
  if (range == BucketRange::Day || range == BucketRange::Week) {
    swprintf_s(buf, L"%d/%d", (key / 100) % 100, key % 100);
  } else if (range == BucketRange::Month) {
    const int m = (key / 100) % 100;
    if (m == 1) swprintf_s(buf, L"%d/%d", key / 10000, 1);
    else swprintf_s(buf, L"%d月", m);
  } else {
    swprintf_s(buf, L"%d", key / 10000);
  }
  return buf;
}

std::wstring OverviewPanel::tipTitle(const BucketF& b) const {
  wchar_t buf[40];
  if (range_ == BucketRange::Day) {
    swprintf_s(buf, L"%d月%d日", (b.beginKey / 100) % 100, b.beginKey % 100);
  } else if (range_ == BucketRange::Week) {
    const int em = (b.endKey / 100) % 100;
    if (em == (b.beginKey / 100) % 100)
      swprintf_s(buf, L"%d月%d日–%d日", em, b.beginKey % 100, b.endKey % 100);
    else
      swprintf_s(buf, L"%d月%d日–%d月%d日", (b.beginKey / 100) % 100, b.beginKey % 100,
                 em, b.endKey % 100);
  } else if (range_ == BucketRange::Month) {
    swprintf_s(buf, L"%d年%d月", b.key / 100, b.key % 100);
  } else {
    swprintf_s(buf, L"%d年", b.key);
  }
  return buf;
}

int OverviewPanel::hit(int x, int y) const {
  if (!open || geomDirty_) return -1;
  const float px = (float)x - rect.left;
  const float py = (float)y - rect.top;
  if (px < 0 || px >= rect.right - rect.left || py < 0 || py >= rect.bottom - rect.top)
    return -1;
  // toast「打开文件夹」链接（显示期间优先命中）
  if (toastUntilMs_ > 0 && px >= toastLinkRc_.left && px < toastLinkRc_.right &&
      py >= toastLinkRc_.top && py < toastLinkRc_.bottom)
    return kToastHit;
  // 面板坐标控件（CloseX/Crumb/RangeTab/KindBtn 在图例/表控件之前入表）
  for (size_t i = 0; i < ctrls_.size(); ++i) {
    const Ctrl& c = ctrls_[i];
    if (c.kind == Ctrl::LegendToggle || c.kind == Ctrl::LegendDrill ||
        c.kind == Ctrl::TableRow)
      break;
    if (px >= c.rc.left && px < c.rc.right && py >= c.rc.top && py < c.rc.bottom)
      return (int)i;
  }
  // 图例（内容坐标 + scrollLegend_）
  if (px >= legendRc_.left && px < legendRc_.right && py >= legendRc_.top &&
      py < legendRc_.bottom) {
    const float cx = px - (legendRc_.left + 8.0f);
    const float cy = py - legendRc_.top + (float)scrollLegend_;
    for (size_t i = 0; i < ctrls_.size(); ++i) {
      const Ctrl& c = ctrls_[i];
      if (c.kind != Ctrl::LegendToggle && c.kind != Ctrl::LegendDrill) continue;
      if (cx >= c.rc.left && cx < c.rc.right && cy >= c.rc.top && cy < c.rc.bottom)
        return (int)i;
    }
    return -1;
  }
  // 明细表（内容坐标 + scrollTable_）
  if (px >= tableRc_.left && px < tableRc_.right && py >= tableRc_.top &&
      py < tableRc_.bottom) {
    const float cx = px - (tableRc_.left + 8.0f);
    const float cy = py - tableRc_.top + (float)scrollTable_;
    for (size_t i = 0; i < ctrls_.size(); ++i) {
      const Ctrl& c = ctrls_[i];
      if (c.kind != Ctrl::TableRow) continue;
      if (cx >= c.rc.left && cx < c.rc.right && cy >= c.rc.top && cy < c.rc.bottom)
        return (int)i;
    }
    return -1;
  }
  return -1;
}

bool OverviewPanel::contains(int x, int y) const {
  if (!open) return false;
  const float px = (float)x - rect.left;
  const float py = (float)y - rect.top;
  return px >= 0 && px < rect.right - rect.left && py >= 0 &&
         py < rect.bottom - rect.top;
}

bool OverviewPanel::titleBarHit(float px, float py) const {
  // 标题栏空白区（按钮除外）= 面板拖拽把手
  if (px < 0 || px >= rect.right - rect.left || py < 0 || py >= kHdH) return false;
  for (size_t i = 0; i < 4 && i < ctrls_.size(); ++i) {
    const D2D1_RECT_F& rc = ctrls_[i].rc;
    if (px >= rc.left && px < rc.right && py >= rc.top && py < rc.bottom) return false;
  }
  return true;
}

bool OverviewPanel::setHover(int ctrlIdx) {
  std::string dim;
  if (ctrlIdx >= 0 && ctrlIdx < (int)ctrls_.size()) {
    const Ctrl& c = ctrls_[(size_t)ctrlIdx];
    if (c.kind == Ctrl::LegendToggle || c.kind == Ctrl::LegendDrill)
      dim = segs_[(size_t)c.a].id;
    else if (c.kind == Ctrl::TableRow)
      dim = trows_[(size_t)c.a].segId;
  }
  if (ctrlIdx == hover && dim == dimSeg_) return false;
  hover = ctrlIdx;
  dimSeg_ = dim;
  return true;
}

bool OverviewPanel::plotHoverAt(float px, float py) {
  // 动画期间禁用悬停 tooltip（拍板 11：直接操作立即终止动画，动画中不弹卡）
  if (!open || geomDirty_ || buckets_.empty() || animType_ != 0 || panning_)
    return clearPlotHover();
  if (px < plotX0_ || px >= plotX0_ + plotW_ || py < plotY0_ ||
      py >= plotY0_ + plotH_)
    return clearPlotHover();
  const double frac = panOffset_ - std::floor(panOffset_);
  const double rel = ((double)plotX0_ + (double)plotW_ - (double)px) / bw_ - 0.5 + frac;
  int i = (int)buckets_.size() - 1 - (int)std::floor(rel);
  i = (std::max)(0, (std::min)(i, (int)buckets_.size() - 1));
  if (i == hoverBucket_ && std::fabs(px - hoverPx_) < 0.5f &&
      std::fabs(py - hoverPy_) < 0.5f)
    return false;
  hoverBucket_ = i;
  hoverPx_ = px;
  hoverPy_ = py;
  return true;
}

bool OverviewPanel::clearPlotHover() {
  if (hoverBucket_ < 0) return false;
  hoverBucket_ = -1;
  return true;
}

bool OverviewPanel::heatHoverAt(float px, float py) {
  if (!open || geomDirty_ || heat_.cells.empty()) return clearHeatHover();
  constexpr float gap = 2.0f;
  const float rx = px - gridX0_, ry = py - gridY0_;
  if (rx < 0 || ry < 0) return clearHeatHover();
  const int col = (int)(rx / (cell_ + gap));
  const int row = (int)(ry / (cell_ + gap));
  if (col < 0 || col >= cols_ || row < 0 || row >= rows_) return clearHeatHover();
  if (rx - (float)col * (cell_ + gap) > cell_ ||
      ry - (float)row * (cell_ + gap) > cell_)  // 缝内不悬停
    return clearHeatHover();
  int i, day = 0;
  if (act_ == ActGrain::Week) {  // 每周：列内任意格 → 同列同 tooltip（当周合计）
    if (col >= (int)heat_.cells.size()) return clearHeatHover();
    day = addDaysK(heat_.cells[(size_t)col].dayKey, row);
    if (day > todayKey_) return clearHeatHover();  // 本周列未来格不悬停
    i = col;
  } else {
    i = col * 7 + row;
    if (i >= (int)heat_.cells.size()) return clearHeatHover();
  }
  if (i == hoverCell_ && day == hoverDay_ && row == hoverRow_ &&
      std::fabs(px - heatPx_) < 0.5f && std::fabs(py - heatPy_) < 0.5f)
    return false;
  hoverCell_ = i;
  hoverDay_ = day;
  hoverRow_ = row;
  heatPx_ = px;
  heatPy_ = py;
  return true;
}

bool OverviewPanel::clearHeatHover() {
  if (hoverCell_ < 0) return false;
  hoverCell_ = -1;
  return true;
}

int OverviewPanel::wheelAt(int x, int y, int delta) {
  if (!open || geomDirty_) return 0;
  const float px = (float)x - rect.left;
  const float py = (float)y - rect.top;
  // 主图 plot：以指针 x 为锚缩放单桶像素宽（下限 = 固定桶数恰好容纳 plot，上限
  // 120px；缩到下限 ≈ 回自适应）。锚点映射：指针下的桶位在缩放前后不动
  if (px >= plotX0_ && px < plotX0_ + plotW_ && py >= plotY0_ &&
      py < plotY0_ + plotH_ && !buckets_.empty()) {
    stopAnim();  // 直接操作立即终止动画落到终态
    const double fit = (double)plotW_ / (double)(std::max)(1, rangeCount_);
    const double old = (double)bw_;
    double next = old * (delta > 0 ? 1.15 : 1.0 / 1.15);
    next = (std::max)(fit, (std::min)(120.0, next));
    if (next <= fit * 1.001) next = fit;
    if (next == old) return 0;
    const double a = (double)plotX0_ + (double)plotW_ - (double)px;
    panOffset_ = (std::max)(0.0, panOffset_ + a * (1.0 / next - 1.0 / old));
    bw_ = (float)next;
    bwCustom_ = next > fit * 1.001;
    maxPan_ = (std::max)(0.0, (double)totalBuckets_ - (double)plotW_ / (double)bw_);
    clampPan();
    return 2;  // 数据窗随 bw 变 → 调用方 refresh + 重排 + 重绘
  }
  const bool inLegend = px >= legendRc_.left && px < legendRc_.right &&
                        py >= legendRc_.top && py < legendRc_.bottom;
  const bool inTable = px >= tableRc_.left && px < tableRc_.right &&
                       py >= tableRc_.top && py < tableRc_.bottom;
  if (!inLegend && !inTable) return 0;
  int& res = inLegend ? wheelResLegend_ : wheelResTable_;
  int& sc = inLegend ? scrollLegend_ : scrollTable_;
  const int contentH = inLegend ? legendContentH_ : tableContentH_;
  const float regionH = inLegend ? legendRc_.bottom - legendRc_.top
                                 : tableRc_.bottom - tableRc_.top;
  const int maxS = (std::max)(0, contentH - (int)regionH);
  res += delta;
  const int step = (res / 120) * 52;  // 一次滚轮 ≈ 2 行
  res %= 120;
  const int ns = (std::min)((std::max)(0, sc - step), maxS);
  if (ns == sc) return 0;
  sc = ns;
  hover = -1;
  dimSeg_.clear();
  return 1;
}

int OverviewPanel::click(int idx) {
  if (idx == kToastHit) return 5;  // toast「打开文件夹」
  if (idx < 0 || idx >= (int)ctrls_.size()) return 0;
  const Ctrl& c = ctrls_[(size_t)idx];
  switch (c.kind) {
  case Ctrl::CloseX:
    return 2;
  case Ctrl::ShareBtn:
    return 4;  // 导出 PNG 分享（app 离屏渲染 + 落盘 + toast）
  case Ctrl::ThemeBtn:
    setTheme(theme_ == "dark" ? "light" : "dark");  // 点击即换整套面板皮肤
    return 3;                                        // app 落盘 overviewTheme
  case Ctrl::PinBtn:
    return 6;  // 置顶切换请求（app 执行 SetWindowPos + 落盘 cfg_.topmost + 回填 setTopmost）
  case Ctrl::RangeTab:
    if ((BucketRange)c.a == range_) return 0;  // 同 tab 重点不重演 morph（防无谓闪帧）
    // 范围 morph：接力——动画中再切，以当前帧实时几何（趋势=采样合成帧）为旧态
    animFrom_ = animType_ == 1 ? animCur_ : buildPlotFrame();
    animFromKind_ = kind_;
    animType_ = 0;
    animCur_ = PlotFrame{};
    range_ = (BucketRange)c.a;
    panOffset_ = 0;        // 切范围 tab 平移归零
    bwCustom_ = false;     // 缩放回自适应（固定桶数恰好容纳）
    hoverBucket_ = -1;
    pendingAnim_ = 1;
    return 1;
  case Ctrl::KindBtn: {
    // 图型切换：150ms 交叉淡入淡出（不做 morph；进行中的 morph 落终态）
    const ChartKind nk = c.a == 0 ? ChartKind::Bars : ChartKind::Trend;
    if (nk == kind_) return 0;
    animFrom_ = animType_ == 1 ? animCur_ : buildPlotFrame();
    animFromKind_ = kind_;
    animType_ = 0;
    animCur_ = PlotFrame{};
    kind_ = nk;
    hoverBucket_ = -1;
    pendingAnim_ = 2;
    return 1;
  }
  case Ctrl::Crumb:
    stopAnim();  // 直接操作立即终止动画落到终态
    drill_.clear();
    hoverBucket_ = -1;
    return 1;
  case Ctrl::ActTab:
    act_ = (ActGrain)c.a;  // 活动图 tabs 不做动画
    hoverCell_ = -1;
    return 1;
  case Ctrl::LegendToggle: {
    stopAnim();
    const Seg& sg = segs_[(size_t)c.a];
    std::set<std::string>& hs = hiddenSet();
    const bool hiding = hs.count(sg.id) == 0;
    if (hiding && visibleCount() <= 1) return 0;  // 保底一项（防全空图）
    if (hiding) hs.insert(sg.id);
    else hs.erase(sg.id);
    return 1;
  }
  case Ctrl::LegendDrill:
    if (!drill_.empty()) return 0;  // 模型层不再下钻
    stopAnim();
    drill_ = segs_[(size_t)c.a].id;
    hoverBucket_ = -1;
    scrollLegend_ = scrollTable_ = 0;
    return 1;
  default:
    return 0;  // TableRow 仅悬停 dim，不可点
  }
}

// ── 拖拽平移 / 动画驱动 ─────────────────────────────────────────────

bool OverviewPanel::panBegin(float px, float py) {
  if (!open || geomDirty_ || buckets_.empty()) return false;
  if (px < plotX0_ || px >= plotX0_ + plotW_ || py < plotY0_ ||
      py >= plotY0_ + plotH_)
    return false;  // 仅 plot 空白处启动（控件命中已由调用方先行分发）
  stopAnim();      // 动画中按下：直接落到终态再拖（原型 pointerdown 同款）
  panArmed_ = true;
  panning_ = false;
  panStartX_ = px;
  panStart_ = panOffset_;
  return true;
}

int OverviewPanel::panMove(float px) {
  if (!panArmed_) return 0;
  const float dx = px - panStartX_;
  if (!panning_) {
    if (std::fabs(dx) < 4.0f) return 0;  // 阈值 <4px 视为悬停不算拖
    panning_ = true;
    clearPlotHover();  // 拖动中不出 tooltip
  }
  // 向右拖 = 翻出更早历史（内容右移，旧桶从左侧进入）：panOffset_ 随像素连续
  // 增减，整数部跨桶边界才换数据窗（返回值 2 由调用方 refresh）
  const double np = panStart_ + (double)dx / (double)bw_;
  const double clamped = (std::max)(0.0, (std::min)(np, maxPan_));
  if (clamped == panOffset_) return 0;
  const int oldBase = (int)std::floor(panOffset_);
  panOffset_ = clamped;
  return (int)std::floor(panOffset_) != oldBase ? 2 : 1;
}

void OverviewPanel::panEnd() {
  panArmed_ = false;
  panning_ = false;
}

void OverviewPanel::stopAnim() {
  animType_ = 0;
  pendingAnim_ = 0;
  animCur_ = PlotFrame{};
  animE_ = 1.0;
}

void OverviewPanel::startPendingAnim(int64_t nowMs) {
  if (pendingAnim_ == 0) return;
  animType_ = pendingAnim_;  // 1=morph 450ms / 2=fade 150ms
  pendingAnim_ = 0;
  animT0_ = nowMs;
  animE_ = 0.0;
  (void)tickAnim(nowMs);  // 立即合成 e=0 首帧（旧态起跳，无跳变）
}

bool OverviewPanel::tickAnim(int64_t nowMs) {
  bool needRender = false;
  if (toastUntilMs_ > 0 && nowMs >= toastUntilMs_) {  // toast 到点自动消失
    toastUntilMs_ = 0;
    needRender = true;  // 须紧跟一帧擦掉
  }
  if (animType_ == 0) return needRender;
  const double dur = animType_ == 1 ? 450.0 : 150.0;
  double t = (double)(nowMs - animT0_) / dur;
  if (t >= 1.0) t = 1.0;
  // morph 用 ease-in-out（长度形变两端都缓，无首帧跳变）；淡化沿用 dock 缓动
  animE_ = animType_ == 1 ? easeInOut(t) : easeDock(t);
  if (animType_ == 1) {
    const PlotFrame to = buildPlotFrame();  // 终态帧随 poll 重取（动画中数据照刷）
    animCur_ = kind_ == ChartKind::Trend ? morphTrend(animFrom_, to, animE_)
                                         : morphBars(animFrom_, to, animE_);
  }
  if (t >= 1.0) {
    animType_ = 0;
    animCur_ = PlotFrame{};
  }
  return true;  // 动画帧全程 + 终帧都需要重绘
}

ID2D1LinearGradientBrush* OverviewPanel::segGrad(const std::string& id,
                                                 chartcolors::Hsl c, bool dim) {
  GradPair& g = grads_[id];
  ComPtr<ID2D1LinearGradientBrush>& slot = dim ? g.dim : g.normal;
  if (slot) return slot.Get();
  const float alpha = dim ? 0.18f : 1.0f;
  const chartcolors::Hsl top{c.h, c.s, (std::min)(100.0f, c.l + 9.0f)};
  const chartcolors::Hsl bot{c.h, c.s, (std::max)(0.0f, c.l - 7.0f)};
  D2D1_GRADIENT_STOP gs[2];
  gs[0] = D2D1::GradientStop(0.0f, segColor(top, alpha));  // 顶亮 +9
  gs[1] = D2D1::GradientStop(1.0f, segColor(bot, alpha));  // 底暗 -7
  ComPtr<ID2D1GradientStopCollection> coll;
  if (FAILED(seen_->CreateGradientStopCollection(gs, 2, &coll))) return nullptr;
  if (FAILED(seen_->CreateLinearGradientBrush(
          D2D1::LinearGradientBrushProperties(D2D1::Point2F(0.0f, 0.0f),
                                              D2D1::Point2F(0.0f, 1.0f)),
          coll.Get(), &slot)))
    return nullptr;
  return slot.Get();
}

void OverviewPanel::frame(const D2D1_RECT_F& rc, const wchar_t* cap) {
  ID2D1DeviceContext* dc = seen_;
  brush_->SetColor(thInk(0.03f));
  const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(rc, 10.0f, 10.0f);
  dc->FillRoundedRectangle(&rr, brush_.Get());
  brush_->SetColor(thInk(0.13f));
  dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
  if (cap) {
    brush_->SetColor(thMuted(1.0f));
    const D2D1_RECT_F tr = D2D1::RectF(rc.left + 10.0f, rc.top + 2.0f, rc.right - 10.0f,
                                        rc.top + 20.0f);
    dc->DrawText(cap, (UINT32)std::wcslen(cap), smallFmt_.Get(), &tr, brush_.Get());
  }
}

void OverviewPanel::draw(render::D3DContext& d3d, render::IMaterial& material) {
  (void)material;  // 面板双主题自供皮（与 dock 材质解耦，不再走 drawCardBack）
  if (!open || !ensure(d3d)) return;
  if (geomDirty_) layout(d3d);
  ID2D1DeviceContext* dc = d3d.dc();
  dc->SetAntialiasMode(D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  dc->SetTextAntialiasMode(D2D1_TEXT_ANTIALIAS_MODE_CLEARTYPE);

  {  // 面板底：主题 --surface + --border（圆角 14）
    brush_->SetColor(th().bg);
    const D2D1_ROUNDED_RECT pr = D2D1::RoundedRect(rect, 14.0f, 14.0f);
    dc->FillRoundedRectangle(&pr, brush_.Get());
    brush_->SetColor(th().border);
    dc->DrawRoundedRectangle(&pr, brush_.Get(), 1.0f);
  }

  // 内容一律面板坐标：整体平移变换（叠加 app 的出现动画变换）
  D2D1_MATRIX_3X2_F baseTm;
  dc->GetTransform(&baseTm);
  dc->SetTransform(D2D1::Matrix3x2F::Translation(rect.left, rect.top) * baseTm);

  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  auto hairline = [&](float x0, float y0, float x1, float y1) {
    brush_->SetColor(thInk(0.13f));
    dc->DrawLine(D2D1::Point2F(x0, y0), D2D1::Point2F(x1, y1), brush_.Get(), 1.0f);
  };
  const float W = rect.right - rect.left;

  // ── 标题栏：accent 圆点 + "OkMeter 用量总览" + OVERVIEW + ✕ ──
  {
    const float cx = 15.0f + 3.5f;
    const float cy = kHdH * 0.5f;
    brush_->SetColor(thAccent(0.30f));
    dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), 5.5f, 5.5f), brush_.Get());
    brush_->SetColor(thAccent(1.0f));
    dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), 3.5f, 3.5f), brush_.Get());
    txt(L"OkMeter 用量总览", titleFmt_.Get(),
        D2D1::RectF(31.0f, 0.0f, 240.0f, kHdH), thInk(0.95f));
    const D2D1_RECT_F& xr = ctrls_[0].rc;
    // OVERVIEW 右界 = 置顶钮左侧（此前到主题钮左侧，被分享钮压字）
    txt(L"OVERVIEW", enFmt_.Get(),
        D2D1::RectF(200.0f, 0.0f, ctrls_[3].rc.left - 9.0f, kHdH), thMuted(1.0f));
    // 主题切换钮（✕ 左侧；月亮=当前浅色点我切深，太阳=当前深色点我切浅）
    {
      const D2D1_RECT_F& tr = ctrls_[1].rc;
      brush_->SetColor(thInk(hover == 1 ? 0.11f : 0.05f));
      const D2D1_ROUNDED_RECT trr = D2D1::RoundedRect(tr, 6.0f, 6.0f);
      dc->FillRoundedRectangle(&trr, brush_.Get());
      brush_->SetColor(thInk(0.13f));
      dc->DrawRoundedRectangle(&trr, brush_.Get(), 1.0f);
      const float gx = (tr.left + tr.right) * 0.5f;
      const float gy = (tr.top + tr.bottom) * 0.5f;
      if (theme_ == "light") {  // 月亮：主圆 - 右上缺口的弯月
        brush_->SetColor(thInk(0.75f));
        dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(gx, gy), 6.0f, 6.0f), brush_.Get());
        brush_->SetColor(th().bg);
        dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(gx + 2.6f, gy - 2.2f), 5.0f, 5.0f),
                        brush_.Get());
      } else {                  // 太阳：中心圆 + 八向光线
        brush_->SetColor(thInk(0.75f));
        dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(gx, gy), 3.6f, 3.6f), brush_.Get());
        for (int k = 0; k < 8; ++k) {
          const float a = 3.14159265f * 0.25f * (float)k;
          const float ca = std::cos(a), sa = std::sin(a);
          dc->DrawLine(D2D1::Point2F(gx + ca * 5.6f, gy + sa * 5.6f),
                       D2D1::Point2F(gx + ca * 8.0f, gy + sa * 8.0f), brush_.Get(), 1.3f);
        }
      }
    }
    // 分享钮（主题钮左侧）：方框 + 右上箭头（导出语义）
    {
      const D2D1_RECT_F& sr = ctrls_[2].rc;
      brush_->SetColor(thInk(hover == 2 ? 0.11f : 0.05f));
      const D2D1_ROUNDED_RECT srr = D2D1::RoundedRect(sr, 6.0f, 6.0f);
      dc->FillRoundedRectangle(&srr, brush_.Get());
      brush_->SetColor(thInk(0.13f));
      dc->DrawRoundedRectangle(&srr, brush_.Get(), 1.0f);
      brush_->SetColor(thInk(hover == 2 ? 0.90f : 0.65f));
      const float gx = (sr.left + sr.right) * 0.5f;
      const float gy = (sr.top + sr.bottom) * 0.5f;
      // 方框（左下开口）+ 自左下向右上引出的箭头
      const float bx0 = gx - 5.5f, by0 = gy - 3.0f, bx1 = gx + 5.5f, by1 = gy + 6.0f;
      dc->DrawLine(D2D1::Point2F(bx0, by0), D2D1::Point2F(bx0, by1), brush_.Get(), 1.3f);
      dc->DrawLine(D2D1::Point2F(bx0, by1), D2D1::Point2F(bx1, by1), brush_.Get(), 1.3f);
      dc->DrawLine(D2D1::Point2F(bx1, by1), D2D1::Point2F(bx1, gy + 0.5f), brush_.Get(), 1.3f);
      dc->DrawLine(D2D1::Point2F(bx0 + 1.0f, gy + 3.2f), D2D1::Point2F(gx + 4.6f, gy - 4.4f),
                   brush_.Get(), 1.3f);
      dc->DrawLine(D2D1::Point2F(gx - 0.6f, gy - 4.6f), D2D1::Point2F(gx + 4.8f, gy - 4.6f),
                   brush_.Get(), 1.3f);
      dc->DrawLine(D2D1::Point2F(gx + 4.8f, gy - 4.6f), D2D1::Point2F(gx + 4.8f, gy + 0.8f),
                   brush_.Get(), 1.3f);
    }
    // 置顶钮（分享钮左侧）：图钉（横头杆 + 竖针）；置顶开 = accent 头杆 + 深针，
    // 关 = 全体降透明。点击切 dock 窗口 WS_EX_TOPMOST（app 落盘 cfg_.topmost）
    {
      const D2D1_RECT_F& pr = ctrls_[3].rc;
      brush_->SetColor(thInk(hover == 3 ? 0.11f : 0.05f));
      const D2D1_ROUNDED_RECT prr = D2D1::RoundedRect(pr, 6.0f, 6.0f);
      dc->FillRoundedRectangle(&prr, brush_.Get());
      brush_->SetColor(thInk(0.13f));
      dc->DrawRoundedRectangle(&prr, brush_.Get(), 1.0f);
      const float gx = (pr.left + pr.right) * 0.5f;
      const float gy = (pr.top + pr.bottom) * 0.5f;
      brush_->SetColor(topmostOn_ ? thAccent(0.95f) : thInk(0.45f));
      const D2D1_ROUNDED_RECT head =
          D2D1::RoundedRect(D2D1::RectF(gx - 5.0f, gy - 4.5f, gx + 5.0f, gy - 1.5f), 1.5f, 1.5f);
      dc->FillRoundedRectangle(&head, brush_.Get());
      brush_->SetColor(topmostOn_ ? thInk(0.90f) : thInk(0.45f));
      dc->DrawLine(D2D1::Point2F(gx, gy - 1.0f), D2D1::Point2F(gx, gy + 6.0f), brush_.Get(),
                   1.6f);
    }
    brush_->SetColor(thInk(hover == 0 ? 0.11f : 0.05f));
    const D2D1_ROUNDED_RECT xrr = D2D1::RoundedRect(xr, 6.0f, 6.0f);
    dc->FillRoundedRectangle(&xrr, brush_.Get());
    brush_->SetColor(thInk(0.13f));
    dc->DrawRoundedRectangle(&xrr, brush_.Get(), 1.0f);
    brush_->SetColor(thInk(hover == 0 ? 0.90f : 0.55f));
    const float gx = (xr.left + xr.right) * 0.5f;
    const float gy = (xr.top + xr.bottom) * 0.5f;
    dc->DrawLine(D2D1::Point2F(gx - 3.5f, gy - 3.5f), D2D1::Point2F(gx + 3.5f, gy + 3.5f),
                 brush_.Get(), 1.3f);
    dc->DrawLine(D2D1::Point2F(gx - 3.5f, gy + 3.5f), D2D1::Point2F(gx + 3.5f, gy - 3.5f),
                 brush_.Get(), 1.3f);
    hairline(0.0f, kHdH - 0.5f, W, kHdH - 0.5f);
  }

  // ── 顶栏：面包屑（下钻时）+ 范围 tabs + 图型分段钮 ──
  {
    size_t ci = 4;  // 控件游标（0=CloseX 1=ThemeBtn 2=ShareBtn 3=PinBtn）
    if (!drill_.empty()) {
      const Ctrl& cr = ctrls_[ci];
      txt(L"‹ 返回提供商", tabFmt_.Get(), cr.rc,
          hover == (int)ci ? thAccent(1.0f) : thAccent(0.75f));
      txt(L"/ " + wide(drill_), smallFmt_.Get(),
          D2D1::RectF(cr.rc.right + 6.0f, cr.rc.top, cr.rc.right + 160.0f, cr.rc.bottom),
          thMuted(1.0f));
      ++ci;
    }
    static const wchar_t* kTabs[] = {L"日", L"周", L"月", L"年"};
    const D2D1_RECT_F t0 = ctrls_[ci].rc, t3 = ctrls_[ci + 3].rc;
    brush_->SetColor(thInk(0.05f));
    const D2D1_ROUNDED_RECT cont =
        D2D1::RoundedRect(D2D1::RectF(t0.left, t0.top, t3.right, t0.bottom), 8.0f, 8.0f);
    dc->FillRoundedRectangle(&cont, brush_.Get());
    brush_->SetColor(thInk(0.13f));
    dc->DrawRoundedRectangle(&cont, brush_.Get(), 1.0f);
    for (int i = 0; i < 4; ++i) {
      const bool on = (int)range_ == i;
      const D2D1_RECT_F cell = ctrls_[ci + (size_t)i].rc;
      if (on || hover == (int)(ci + (size_t)i)) {
        brush_->SetColor(on ? thAccent(0.16f) : thInk(0.07f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(
            D2D1::RectF(cell.left + 2.0f, cell.top + 2.0f, cell.right - 2.0f,
                        cell.bottom - 2.0f),
            6.0f, 6.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        if (on) {
          brush_->SetColor(thAccent(0.65f));
          dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        }
      }
      txt(kTabs[i], tabFmt_.Get(), cell, thInk(on ? 0.95f : 0.55f));
    }
    ci += 4;
    static const wchar_t* kKinds[] = {L"柱状", L"趋势"};
    const D2D1_RECT_F k0 = ctrls_[ci].rc, k1 = ctrls_[ci + 1].rc;
    brush_->SetColor(thInk(0.05f));
    const D2D1_ROUNDED_RECT kcont =
        D2D1::RoundedRect(D2D1::RectF(k0.left, k0.top, k1.right, k0.bottom), 8.0f, 8.0f);
    dc->FillRoundedRectangle(&kcont, brush_.Get());
    brush_->SetColor(thInk(0.13f));
    dc->DrawRoundedRectangle(&kcont, brush_.Get(), 1.0f);
    for (int i = 0; i < 2; ++i) {
      const bool on = (kind_ == ChartKind::Trend) == (i == 1);
      const D2D1_RECT_F cell = ctrls_[ci + (size_t)i].rc;
      if (on || hover == (int)(ci + (size_t)i)) {
        brush_->SetColor(on ? thAccent(0.16f) : thInk(0.07f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(
            D2D1::RectF(cell.left + 2.0f, cell.top + 2.0f, cell.right - 2.0f,
                        cell.bottom - 2.0f),
            6.0f, 6.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        if (on) {
          brush_->SetColor(thAccent(0.65f));
          dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        }
      }
      txt(kKinds[i], tabFmt_.Get(), cell, thInk(on ? 0.95f : 0.55f));
    }
  }

  // ── 四区域容器 + 主图/图例/明细内容 ──
  frame(mainRc_, nullptr);
  drawChart(d3d, material);
  frame(legendRc_, L"图例");
  drawLegend(d3d);
  frame(tableRc_, L"明细表");
  drawTable(d3d);
  frame(actRc_, nullptr);
  {  // 活动图 cap + 粒度 tabs（每日/每周/累计，不做动画）
    brush_->SetColor(thMuted(1.0f));
    const D2D1_RECT_F capRc = D2D1::RectF(actRc_.left + 10.0f, actRc_.top + 2.0f,
                                          actRc_.left + 160.0f, actRc_.top + 20.0f);
    dc->DrawText(L"Token 活动", 10, smallFmt_.Get(), &capRc, brush_.Get());
    static const wchar_t* kActs[] = {L"每日", L"每周", L"累计"};
    for (size_t i = 0; i < ctrls_.size(); ++i) {
      const Ctrl& c = ctrls_[i];
      if (c.kind != Ctrl::ActTab) continue;
      const bool on = (int)act_ == c.a;
      if (on || hover == (int)i) {
        brush_->SetColor(on ? thAccent(0.16f) : thInk(0.07f));
        const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(c.rc, 6.0f, 6.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        if (on) {
          brush_->SetColor(thAccent(0.65f));
          dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
        }
      }
      txt(kActs[c.a], tabFmt_.Get(), c.rc, thInk(on ? 0.95f : 0.55f));
    }
  }
  drawActivity(d3d);
  drawStats(d3d);
  drawTooltip(d3d, material);
  drawHeatTip(d3d, material);
  drawToast(d3d);

  // ── 底注：真实数据起点 + token 口径 ──
  {
    const float ftY = rect.bottom - rect.top - kFtH;
    hairline(0.0f, ftY + 0.5f, W, ftY + 0.5f);
    txt(foot_, footFmt_.Get(), D2D1::RectF(15.0f, ftY, W - 15.0f, ftY + kFtH),
        thMuted(1.0f));
  }

  dc->SetTransform(baseTm);
}

OverviewPanel::PlotFrame OverviewPanel::buildPlotFrame() const {
  PlotFrame f;
  f.bw = bw_;
  f.yMax = (double)yMax_;
  f.range = range_;
  f.segs = segs_;  // 帧携带自己的分段清单（morph 两端可能不同，按 id 对齐）
  f.keys.reserve(buckets_.size());
  f.xs.reserve(buckets_.size());
  f.vals.reserve(buckets_.size());
  for (size_t i = 0; i < buckets_.size(); ++i) {
    f.keys.push_back(buckets_[i].beginKey);  // 形态键：日=当日、周=周一、月=月首、年=元旦
    f.xs.push_back(frameBucketX(i));
    std::vector<double> v;  // int64→double 显式转换（迭代器范围构造会触发 C4244）
    v.reserve(buckets_[i].segVals.size());
    for (const int64_t x : buckets_[i].segVals) v.push_back((double)x);
    f.vals.push_back(std::move(v));
  }
  return f;
}

// 柱状 morph：键日期并集插值——共有键 x/分段值双 lerp；仅新帧有的键从基线长出
//（值 0 起）、仅旧帧有的键沉回基线（原型 interpolateFrame 同款）。
// 分段清单取两端并集（按 id 对齐）：一端才有的分段以 0 值起/沉，颜色一律取
// 各自色板（日⇄年窗口分段数不同，下标错位会读出越界值画成满高怪柱）
OverviewPanel::PlotFrame OverviewPanel::morphBars(const PlotFrame& o, const PlotFrame& n,
                                                  double e) const {
  auto keyIdx = [](const PlotFrame& f, int key) -> int {
    for (size_t i = 0; i < f.keys.size(); ++i)
      if (f.keys[i] == key) return (int)i;
    return -1;
  };
  auto segIdx = [](const PlotFrame& f, const std::string& id) -> int {
    for (size_t j = 0; j < f.segs.size(); ++j)
      if (f.segs[j].id == id) return (int)j;
    return -1;
  };
  PlotFrame out;
  out.bw = n.bw;
  out.yMax = o.yMax + (n.yMax - o.yMax) * e;
  out.range = n.range;
  out.segs = n.segs;  // 新帧分段为主，旧帧独有分段追加（沉回基线用，色板保真）
  for (const Seg& s : o.segs)
    if (segIdx(n, s.id) < 0) out.segs.push_back(s);
  std::vector<int> keys = o.keys;
  for (int k : n.keys)
    if (keyIdx(o, k) < 0) keys.push_back(k);
  for (int k : keys) {
    const int io = keyIdx(o, k), in = keyIdx(n, k);
    const float xo = io >= 0 ? o.xs[(size_t)io] : n.xs[(size_t)in];
    const float xn = in >= 0 ? n.xs[(size_t)in] : o.xs[(size_t)io];
    out.keys.push_back(k);
    out.xs.push_back((float)(xo + (xn - xo) * e));
    std::vector<double> v(out.segs.size(), 0.0);
    for (size_t j = 0; j < out.segs.size(); ++j) {
      const int jo = segIdx(o, out.segs[j].id);
      const int jn = segIdx(n, out.segs[j].id);
      const double vo = io >= 0 && jo >= 0 ? o.vals[(size_t)io][(size_t)jo] : 0.0;
      const double vn = in >= 0 && jn >= 0 ? n.vals[(size_t)in][(size_t)jn] : 0.0;
      v[j] = vo + (vn - vo) * e;
    }
    out.vals.push_back(std::move(v));
  }
  // x 排序（插值中键序可能交错）
  std::vector<size_t> ord(out.keys.size());
  for (size_t i = 0; i < ord.size(); ++i) ord[i] = i;
  std::sort(ord.begin(), ord.end(),
            [&](size_t a, size_t b) { return out.xs[a] < out.xs[b]; });
  PlotFrame sorted;
  sorted.bw = out.bw;
  sorted.yMax = out.yMax;
  sorted.range = out.range;
  sorted.segs = std::move(out.segs);
  for (size_t i : ord) {
    sorted.keys.push_back(out.keys[i]);
    sorted.xs.push_back(out.xs[i]);
    sorted.vals.push_back(std::move(out.vals[i]));
  }
  return sorted;
}

// 趋势 morph：函数 y(x) 形状插值（v1 教训：禁键并集穿线——会 zigzag；禁满宽外推
// 采样——终帧"突然变短"）。新旧曲线各自在自身跨度内取 64 采样点逐点 lerp，
// 水平跨度（首末桶中心距）同步插值；接力调用时 o = 当前显示的采样合成帧
OverviewPanel::PlotFrame OverviewPanel::morphTrend(const PlotFrame& o, const PlotFrame& n,
                                                   double e) const {
  constexpr int kSamples = 64;
  auto extent = [](const PlotFrame& f, float& lo, float& hi) {
    lo = 1e30f;
    hi = -1e30f;
    for (float x : f.xs) {
      if (x < lo) lo = x;
      if (x > hi) hi = x;
    }
    if (lo > 1e29f || hi - lo < 1.0f) {
      const float c = lo > 1e29f ? 0.0f : (lo + hi) * 0.5f;
      lo = c - 0.5f;
      hi = c + 0.5f;
    }
  };
  float ao, bo, an, bn;
  extent(o, ao, bo);
  extent(n, an, bn);
  // 分段清单取两端并集（按 id）：一端才有的分段按 0 值曲线起/沉，
  // 每条曲线只在自己的键集上取 FC（缺失帧给空点列 → 恒 0，不串段不错位）
  PlotFrame out;
  out.bw = n.bw;
  out.yMax = o.yMax + (n.yMax - o.yMax) * e;
  out.range = n.range;
  out.segs = n.segs;
  for (const Seg& s : o.segs) {
    bool found = false;
    for (const Seg& t : n.segs)
      if (t.id == s.id) { found = true; break; }
    if (!found) out.segs.push_back(s);
  }
  std::vector<FcEval> evO, evN;
  for (const Seg& s : out.segs) {
    std::vector<D2D1_POINT_2F> po, pn;
    for (size_t j = 0; j < o.segs.size(); ++j)
      if (o.segs[j].id == s.id) {
        for (size_t k = 0; k < o.keys.size(); ++k)
          po.push_back(D2D1::Point2F(o.xs[k], (float)o.vals[k][j]));
        break;
      }
    for (size_t j = 0; j < n.segs.size(); ++j)
      if (n.segs[j].id == s.id) {
        for (size_t k = 0; k < n.keys.size(); ++k)
          pn.push_back(D2D1::Point2F(n.xs[k], (float)n.vals[k][j]));
        break;
      }
    evO.push_back(fcCurve(po));
    evN.push_back(fcCurve(pn));
  }
  for (int i = 0; i < kSamples; ++i) {
    const float t = (float)i / (float)(kSamples - 1);
    const float xo = ao + (bo - ao) * t;
    const float xn = an + (bn - an) * t;
    out.keys.push_back(i);  // 合成键（刻度标签由 drawChart 以新帧键另行绘制）
    out.xs.push_back((float)(xo + (xn - xo) * e));
    std::vector<double> v(out.segs.size(), 0.0);
    for (size_t j = 0; j < out.segs.size(); ++j)
      v[j] = evO[j](xo) + (evN[j](xn) - evO[j](xo)) * e;
    out.vals.push_back(std::move(v));
  }
  return out;
}

void OverviewPanel::drawChart(render::D3DContext& d3d, render::IMaterial& material) {
  (void)material;
  if (animType_ == 2) {  // 图型切换：150ms 交叉淡入淡出（不做 morph）
    drawPlot(d3d, animFrom_, animFromKind_, (float)(1.0 - animE_));
    const PlotFrame cur = buildPlotFrame();
    drawPlot(d3d, cur, kind_, (float)animE_);
    drawXLabels(d3d, animFrom_, (float)(1.0 - animE_));
    drawXLabels(d3d, cur, (float)animE_);
    return;
  }
  if (animType_ == 1) {  // 范围切换 morph：合成帧内容；刻度新旧交叉淡化（防跳变闪帧）
    const PlotFrame cur = buildPlotFrame();
    drawPlot(d3d, animCur_, kind_, 1.0f);
    drawXLabels(d3d, animFrom_, (float)(1.0 - animE_));
    drawXLabels(d3d, cur, (float)animE_);
    return;
  }
  const PlotFrame cur = buildPlotFrame();
  drawPlot(d3d, cur, kind_, 1.0f);
  drawXLabels(d3d, cur, 1.0f);
}

void OverviewPanel::drawPlot(render::D3DContext& d3d, const PlotFrame& f,
                             ChartKind kind, float alpha) {
  ID2D1DeviceContext* dc = d3d.dc();
  const bool layered = alpha < 0.999f;
  if (layered) {  // 淡化：整帧（网格+内容）走透明度层
    const D2D1_LAYER_PARAMETERS lp = D2D1::LayerParameters(
        D2D1::InfiniteRect(), nullptr, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
        D2D1::IdentityMatrix(), alpha);
    dc->PushLayer(&lp, nullptr);
  }
  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  // 网格线 + Y 轴千分位刻度（Consolas，右对齐；yMax 随 morph 插值逐帧重算不瞬跳）。
  // 标签走 DrawTextLayout（布局宽 4000、trailing 右对齐、越界不裁不折行）：morph
  // 中间帧的 yMax 千分位串（如 3,200,000,000）比按终态测量的 yLabelW 宽，
  // DrawText 矩形不够宽时会折行成孤立的"0"
  const int64_t step = niceStep((std::max)((int64_t)1, (int64_t)(f.yMax / 4.0)));
  const float labelX1 = plotX0_ - 8.0f;
  for (int64_t v = step; (double)v <= f.yMax + (double)step * 0.5; v += step) {
    const float y = (float)plotYOf(f.yMax, (double)v);
    brush_->SetColor(thInk(0.07f));
    dc->DrawLine(D2D1::Point2F(plotX0_, y), D2D1::Point2F(plotX0_ + plotW_, y),
                 brush_.Get(), 1.0f);
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
    const std::wstring label = wide(fmtExact(v));
    ComPtr<IDWriteTextLayout> tl;
    if (SUCCEEDED(d3d.dwrite()->CreateTextLayout(label.c_str(), (UINT32)label.size(),
                                                 monoFmt_.Get(), 4000.0f, 20.0f, &tl))) {
      brush_->SetColor(thMuted(1.0f));
      dc->DrawTextLayout(D2D1::Point2F(labelX1 - 4000.0f, y - 7.0f), tl.Get(),
                         brush_.Get());
    } else {
      txt(label, monoFmt_.Get(),
          D2D1::RectF((std::max)(4.0f, labelX1 - 120.0f), y - 7.0f, labelX1, y + 7.0f),
          thMuted(1.0f));
    }
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
  }
  // 独立基线（比网格线亮）
  brush_->SetColor(thInk(0.26f));
  const float yBase = (float)plotYOf(f.yMax, 0.0);
  dc->DrawLine(D2D1::Point2F(plotX0_, yBase), D2D1::Point2F(plotX0_ + plotW_, yBase),
               brush_.Get(), 1.0f);
  // 悬停列背景高亮（柱状，rx3；动画期间禁用悬停 tooltip/高亮）
  if (kind == ChartKind::Bars && animType_ == 0 && hoverBucket_ >= 0 &&
      hoverBucket_ < (int)f.keys.size()) {
    const float cx = f.xs[(size_t)hoverBucket_];
    brush_->SetColor(thInk(0.05f));
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(
        D2D1::RectF(cx - f.bw * 0.5f, plotY0_, cx + f.bw * 0.5f, plotY0_ + plotH_),
        3.0f, 3.0f);
    dc->FillRoundedRectangle(&rr, brush_.Get());
  }
  // 内容一律裁进 plot 矩形（原型教训：无裁剪柱身画进 Y 轴刻度区）
  const D2D1_RECT_F plotRc =
      D2D1::RectF(plotX0_, plotY0_, plotX0_ + plotW_, plotY0_ + plotH_);
  dc->PushAxisAlignedClip(&plotRc, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  if (kind == ChartKind::Bars) drawBarsF(dc, f);
  else drawTrendF(dc, f);
  dc->PopAxisAlignedClip();
  if (layered) dc->PopLayer();
}

// X 轴刻度独立分层（morph/淡化时新旧两套按 alpha 交叉淡出淡入，防跳变闪帧）：
// 仅中心在 plot 内的键，过密抽稀，末键必画
void OverviewPanel::drawXLabels(render::D3DContext& d3d, const PlotFrame& lf,
                                float alpha) {
  if (alpha <= 0.001f || lf.keys.empty()) return;
  ID2D1DeviceContext* dc = d3d.dc();
  const bool layered = alpha < 0.999f;
  if (layered) {
    const D2D1_LAYER_PARAMETERS lp = D2D1::LayerParameters(
        D2D1::InfiniteRect(), nullptr, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE,
        D2D1::IdentityMatrix(), alpha);
    dc->PushLayer(&lp, nullptr);
  }
  std::vector<int> labeled;
  for (size_t i = 0; i < lf.keys.size(); ++i)
    if (lf.xs[i] >= plotX0_ + 0.5f && lf.xs[i] <= plotX0_ + plotW_ - 0.5f)
      labeled.push_back((int)i);
  const int every = (std::max)(1, (int)((labeled.size() + 7) / 8));
  for (size_t k = 0; k < labeled.size(); ++k) {
    if ((int)k % every != 0 && k != labeled.size() - 1) continue;
    const float cx = lf.xs[(size_t)labeled[k]];
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);
    brush_->SetColor(thMuted(1.0f));
    const std::wstring t = tickLabelForKey(lf.keys[(size_t)labeled[k]], lf.range);
    const D2D1_RECT_F tr = D2D1::RectF(cx - 40.0f, plotY0_ + plotH_ + 5.0f,
                                        cx + 40.0f, plotY0_ + plotH_ + 19.0f);
    dc->DrawText(t.c_str(), (UINT32)t.size(), monoFmt_.Get(), &tr, brush_.Get());
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
  }
  if (layered) dc->PopLayer();
}

void OverviewPanel::drawBarsF(ID2D1DeviceContext* dc, const PlotFrame& f) {
  const bool dimAny = !dimSeg_.empty();
  const float barW =
      (std::max)(2.0f, (std::min)((std::min)(f.bw * 0.52f, 34.0f), f.bw - 2.0f));
  const float yPlotTop = plotY0_, yPlotBot = plotY0_ + plotH_;
  for (size_t i = 0; i < f.keys.size(); ++i) {
    const float cx = f.xs[i];
    if (cx + barW * 0.5f < plotX0_ || cx - barW * 0.5f > plotX0_ + plotW_) continue;
    struct Part { int seg; float yTop, yBot; };
    std::vector<Part> parts;
    double acc = 0;
    for (size_t j = 0; j < f.segs.size(); ++j) {
      if (f.segs[j].hidden) continue;
      const double v = f.vals[i][j];
      if (!(v > 0.01)) continue;  // NaN/负值防御（比较为假即跳过）
      float yTop = (float)plotYOf(f.yMax, acc + v);
      float yBot = (float)plotYOf(f.yMax, acc);
      // 柱高对 [0, 当前 yMax] 夹取（morph 中值可超出本帧 yMax 插值，防满高怪柱）
      yTop = (std::max)(yPlotTop, (std::min)(yTop, yPlotBot));
      yBot = (std::max)(yPlotTop, (std::min)(yBot, yPlotBot));
      parts.push_back(Part{(int)j, yTop, yBot});
      acc += v;
    }
    for (size_t pi = 0; pi < parts.size(); ++pi) {
      const Part& pt = parts[pi];
      const bool isBottom = pi == 0, isTop = pi + 1 == parts.size();
      const float x = cx - barW * 0.5f;
      const float h = (std::max)(1.0f, pt.yBot - pt.yTop - (isBottom ? 0.0f : 1.0f));  // 1px 呼吸缝
      const Seg& sg = f.segs[(size_t)pt.seg];
      const bool dim = dimAny && sg.id != dimSeg_;
      ID2D1LinearGradientBrush* g = segGrad(sg.id, sg.hsl, dim);
      if (!g) continue;
      // D2D 行向量连乘：先 Scale(1,h) 铺满段高，再 Translation 落位
      g->SetTransform(D2D1::Matrix3x2F::Scale(1.0f, h) *
                      D2D1::Matrix3x2F::Translation(x, pt.yTop));
      const float rt = isTop ? (std::max)(0.0f, (std::min)((std::min)(3.5f, barW * 0.5f), h * 0.5f)) : 0.0f;
      if (rt > 0.5f) {
        // 最顶段上沿圆角：整段圆角矩形 + 下半补方（同画刷同变换，视觉无缝）
        const D2D1_ROUNDED_RECT rr =
            D2D1::RoundedRect(D2D1::RectF(x, pt.yTop, x + barW, pt.yTop + h), rt, rt);
        dc->FillRoundedRectangle(&rr, g);
        dc->FillRectangle(D2D1::RectF(x, pt.yTop + rt, x + barW, pt.yTop + h), g);
      } else {
        dc->FillRectangle(D2D1::RectF(x, pt.yTop, x + barW, pt.yTop + h), g);
      }
    }
  }
}

void OverviewPanel::drawTrendF(ID2D1DeviceContext* dc, const PlotFrame& f) {
  const bool dimAny = !dimSeg_.empty();
  const bool hasHover = animType_ == 0 && hoverBucket_ >= 0 &&
                        hoverBucket_ < (int)f.keys.size();
  for (size_t j = 0; j < f.segs.size(); ++j) {
    if (f.segs[j].hidden) continue;
    const float alpha = dimAny && f.segs[j].id != dimSeg_ ? 0.18f : 1.0f;
    std::vector<D2D1_POINT_2F> pts;
    pts.reserve(f.keys.size());
    for (size_t i = 0; i < f.keys.size(); ++i)
      pts.push_back(D2D1::Point2F(f.xs[i], (float)plotYOf(f.yMax, f.vals[i][j])));
    brush_->SetColor(segColor(f.segs[j].hsl, alpha));
    if (pts.size() == 1) {  // 单点分段画圆点
      dc->FillEllipse(D2D1::Ellipse(pts[0], 3.0f, 3.0f), brush_.Get());
    } else if (!pts.empty()) {
      const std::vector<float> m = fcTangents(pts);
      ComPtr<ID2D1Factory> fac;
      dc->GetFactory(&fac);
      ComPtr<ID2D1PathGeometry> geo;
      ComPtr<ID2D1GeometrySink> sink;
      if (fac && SUCCEEDED(fac->CreatePathGeometry(&geo)) && SUCCEEDED(geo->Open(&sink))) {
        sink->BeginFigure(pts[0], D2D1_FIGURE_BEGIN_HOLLOW);
        for (size_t i = 0; i + 1 < pts.size(); ++i) {
          const float h = pts[i + 1].x - pts[i].x;
          sink->AddBezier(D2D1::BezierSegment(
              D2D1::Point2F(pts[i].x + h / 3.0f, pts[i].y + m[i] * h / 3.0f),
              D2D1::Point2F(pts[i + 1].x - h / 3.0f, pts[i + 1].y - m[i + 1] * h / 3.0f),
              pts[i + 1]));
        }
        sink->EndFigure(D2D1_FIGURE_END_OPEN);
        if (SUCCEEDED(sink->Close()))
          dc->DrawGeometry(geo.Get(), brush_.Get(), 2.0f);  // 线宽 2 圆头
      }
    }
    if (hasHover) {  // 悬停桶描点
      const float cx = f.xs[(size_t)hoverBucket_];
      const float cy = (float)plotYOf(f.yMax, f.vals[(size_t)hoverBucket_][j]);
      dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), 3.2f, 3.2f), brush_.Get());
      D2D1_COLOR_F ring = th().surface;  // 描点描边 = 主题 surface（--c-ring）
      ring.a = alpha;
      brush_->SetColor(ring);
      dc->DrawEllipse(D2D1::Ellipse(D2D1::Point2F(cx, cy), 3.2f, 3.2f), brush_.Get(),
                      1.0f);
    }
  }
  if (hasHover) {  // 垂直虚线参考线（吸附最近桶中心）
    const float cx = f.xs[(size_t)hoverBucket_];
    brush_->SetColor(thInk(0.30f));
    dc->DrawLine(D2D1::Point2F(cx, plotY0_), D2D1::Point2F(cx, plotY0_ + plotH_),
                 brush_.Get(), 1.0f, dashStyle_.Get());
  }
}

void OverviewPanel::drawTooltip(render::D3DContext& d3d, render::IMaterial& material) {
  if (hoverBucket_ < 0 || hoverBucket_ >= (int)buckets_.size()) return;
  ID2D1DeviceContext* dc = d3d.dc();
  const BucketF& b = buckets_[(size_t)hoverBucket_];
  struct Row { const Seg* sg; int64_t v; };
  std::vector<Row> rows;
  int64_t total = 0;
  for (size_t j = 0; j < segs_.size(); ++j) {
    if (segs_[j].hidden || b.segVals[j] <= 0) continue;
    rows.push_back(Row{&segs_[j], b.segVals[j]});
    total += b.segVals[j];
  }
  std::stable_sort(rows.begin(), rows.end(),
                   [](const Row& x, const Row& y) { return x.v > y.v; });
  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  auto mwidth = [&](IDWriteTextFormat* fmt, const std::wstring& s) {
    ComPtr<IDWriteTextLayout> tl;
    if (SUCCEEDED(d3d.dwrite()->CreateTextLayout(s.c_str(), (UINT32)s.size(), fmt,
                                                 2000.0f, 100.0f, &tl))) {
      DWRITE_TEXT_METRICS m{};
      if (SUCCEEDED(tl->GetMetrics(&m))) return m.width;
    }
    return measure(fmt, s);
  };
  const std::wstring title = tipTitle(b);
  float cw = mwidth(tipFmt_.Get(), title);
  for (const Row& r : rows) {
    const float w = 16.0f + mwidth(smallFmt_.Get(), wide(r.sg->label)) + 18.0f +
                    mwidth(monoFmt_.Get(), wide(fmtExact(r.v)));
    if (w > cw) cw = w;
  }
  const float sumW = 40.0f + mwidth(monoFmt_.Get(), wide(fmtExact(total)));
  if (sumW > cw) cw = sumW;
  const float cardW = cw + 22.0f;
  const float cardH = 10.0f + 18.0f + (float)rows.size() * 17.0f + 4.0f + 18.0f + 8.0f;
  const float W = rect.right - rect.left;
  const float H = rect.bottom - rect.top;
  float x = hoverPx_ + 16.0f, y = hoverPy_ + 14.0f;  // 面板坐标，夹取面板内
  if (x + cardW > W - 8.0f) x = hoverPx_ - cardW - 14.0f;
  if (x < 8.0f) x = 8.0f;
  if (y + cardH > H - 8.0f) y = hoverPy_ - cardH - 12.0f;
  if (y < 8.0f) y = 8.0f;
  // 面板坐标 → 窗口坐标（当前变换含 rect 平移，卡底要绝对坐标）
  D2D1_MATRIX_3X2_F tm;
  dc->GetTransform(&tm);
  dc->SetTransform(D2D1::IdentityMatrix());
  const D2D1_RECT_F cardRc = D2D1::RectF(rect.left + x, rect.top + y,
                                          rect.left + x + cardW, rect.top + y + cardH);
  (void)material;  // tooltip 卡底走主题 --surface（与面板同源，不经材质）
  brush_->SetColor(th().surface);
  const D2D1_ROUNDED_RECT crr = D2D1::RoundedRect(cardRc, 12.0f, 12.0f);
  dc->FillRoundedRectangle(&crr, brush_.Get());
  brush_->SetColor(thInk(0.13f));
  const D2D1_ROUNDED_RECT brr = D2D1::RoundedRect(cardRc, 12.0f, 12.0f);
  dc->DrawRoundedRectangle(&brr, brush_.Get(), 1.0f);
  dc->SetTransform(tm);
  txt(title, tipFmt_.Get(), D2D1::RectF(x + 11.0f, y + 6.0f, x + cardW - 8.0f, y + 24.0f),
      thInk(0.92f));
  float ry = y + 26.0f;
  for (const Row& r : rows) {
    brush_->SetColor(segColor(r.sg->hsl, 1.0f));
    dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(x + 14.5f, ry + 8.5f), 3.5f, 3.5f),
                    brush_.Get());
    txt(wide(r.sg->label), smallFmt_.Get(),
        D2D1::RectF(x + 22.0f, ry, x + cardW - 60.0f, ry + 17.0f), thInk(0.80f));
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
    txt(wide(fmtExact(r.v)), monoFmt_.Get(),
        D2D1::RectF(x + cardW - 90.0f, ry, x + cardW - 11.0f, ry + 17.0f),
        thInk(0.92f));
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    ry += 17.0f;
  }
  brush_->SetColor(thInk(0.13f));
  dc->DrawLine(D2D1::Point2F(x + 11.0f, ry + 2.0f), D2D1::Point2F(x + cardW - 11.0f, ry + 2.0f),
               brush_.Get(), 1.0f);
  txt(L"合计", smallFmt_.Get(), D2D1::RectF(x + 22.0f, ry + 4.0f, x + 70.0f, ry + 21.0f),
      thInk(0.55f));
  monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
  txt(wide(fmtExact(total)), monoFmt_.Get(),
      D2D1::RectF(x + cardW - 100.0f, ry + 4.0f, x + cardW - 11.0f, ry + 21.0f),
      thInk(0.95f));
  monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
}

void OverviewPanel::drawLegend(render::D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  if (segs_.empty()) {
    txt(L"当前窗口无数据", smallFmt_.Get(),
        D2D1::RectF(legendRc_.left + 10.0f, legendRc_.top + 22.0f, legendRc_.right - 10.0f,
                    legendRc_.top + 40.0f),
        thMuted(1.0f));
    return;
  }
  dc->PushAxisAlignedClip(&legendRc_, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  D2D1_MATRIX_3X2_F tm;
  dc->GetTransform(&tm);
  dc->SetTransform(D2D1::Matrix3x2F::Translation(legendRc_.left + 8.0f,
                                                 legendRc_.top - (float)scrollLegend_) *
                   tm);
  const float cw = (legendRc_.right - legendRc_.left) - 16.0f - 10.0f;
  const int visN = visibleCount();
  for (size_t i = 0; i < segs_.size(); ++i) {
    const Seg& sg = segs_[i];
    const float y = 22.0f + (float)i * kLegendRowH;
    const bool hov = hover >= 0 && hover < (int)ctrls_.size() &&
                     (ctrls_[(size_t)hover].kind == Ctrl::LegendToggle ||
                      ctrls_[(size_t)hover].kind == Ctrl::LegendDrill) &&
                     ctrls_[(size_t)hover].a == (int)i;
    const bool locked = !sg.hidden && visN <= 1;  // 保底一项：最后一项禁用态
    if (hov) {
      brush_->SetColor(thInk(0.06f));
      const D2D1_ROUNDED_RECT rr =
          D2D1::RoundedRect(D2D1::RectF(0.0f, y, cw, y + kLegendRowH), 6.0f, 6.0f);
      dc->FillRoundedRectangle(&rr, brush_.Get());
    }
    // 色块（显隐钮）：隐藏 = 描边空块；保底锁定 = 降 alpha
    const D2D1_RECT_F sw = D2D1::RectF(5.0f, y + 7.0f, 17.0f, y + 19.0f);
    const float swA = sg.hidden ? 0.35f : locked ? 0.45f : 1.0f;
    const D2D1_ROUNDED_RECT swrr = D2D1::RoundedRect(sw, 3.0f, 3.0f);
    if (!sg.hidden) {
      brush_->SetColor(segColor(sg.hsl, swA));
      dc->FillRoundedRectangle(&swrr, brush_.Get());
    } else {
      brush_->SetColor(segColor(sg.hsl, 0.55f));
      dc->DrawRoundedRectangle(&swrr, brush_.Get(), 1.2f);
    }
    const float inkA = sg.hidden ? 0.35f : 0.90f;
    txt(wide(sg.label), bodyFmt_.Get(), D2D1::RectF(24.0f, y, cw - 80.0f, y + kLegendRowH),
        thInk(inkA));
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
    txt(wide(fmtExact(sg.windowTotal)), monoFmt_.Get(),
        D2D1::RectF(cw - 96.0f, y, cw - (drill_.empty() ? 20.0f : 4.0f), y + kLegendRowH),
        thInk(sg.hidden ? 0.30f : 0.55f));
    monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    if (drill_.empty()) {  // › 下钻标（两笔描边画，字形回退不稳）
      brush_->SetColor(thInk(hov ? 0.75f : 0.40f));
      const float ax = cw - 10.0f;
      const float ay = y + kLegendRowH * 0.5f;
      dc->DrawLine(D2D1::Point2F(ax - 1.6f, ay - 4.0f), D2D1::Point2F(ax + 2.4f, ay),
                   brush_.Get(), 1.3f);
      dc->DrawLine(D2D1::Point2F(ax + 2.4f, ay), D2D1::Point2F(ax - 1.6f, ay + 4.0f),
                   brush_.Get(), 1.3f);
    }
  }
  dc->SetTransform(tm);
  dc->PopAxisAlignedClip();
  // 滚动条（settings 同款 3px 拇指）
  const int maxS = (std::max)(0, legendContentH_ - (int)(legendRc_.bottom - legendRc_.top));
  if (maxS > 0) {
    const float trackH = (legendRc_.bottom - legendRc_.top) - 8.0f;
    const float th = (std::max)(24.0f, trackH * (legendRc_.bottom - legendRc_.top) /
                                             (float)legendContentH_);
    const float ty = legendRc_.top + 4.0f + (trackH - th) * (float)scrollLegend_ / (float)maxS;
    brush_->SetColor(thInk(0.25f));
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(
        D2D1::RectF(legendRc_.right - 6.0f, ty, legendRc_.right - 3.0f, ty + th),
        1.5f, 1.5f);
    dc->FillRoundedRectangle(&rr, brush_.Get());
  }
}

void OverviewPanel::drawTable(render::D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  if (trows_.empty()) {
    txt(L"当前窗口无数据", smallFmt_.Get(),
        D2D1::RectF(tableRc_.left + 10.0f, tableRc_.top + 22.0f, tableRc_.right - 10.0f,
                    tableRc_.top + 40.0f),
        thMuted(1.0f));
    return;
  }
  dc->PushAxisAlignedClip(&tableRc_, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  D2D1_MATRIX_3X2_F tm;
  dc->GetTransform(&tm);
  dc->SetTransform(D2D1::Matrix3x2F::Translation(tableRc_.left + 8.0f,
                                                 tableRc_.top - (float)scrollTable_) *
                   tm);
  const float cw = (tableRc_.right - tableRc_.left) - 16.0f - 10.0f;
  float y = 22.0f;
  for (size_t i = 0; i < trows_.size(); ++i) {
    const TRow& r = trows_[i];
    const float h = r.group ? kGroupRowH : kModelRowH;
    const bool hov = hover >= 0 && hover < (int)ctrls_.size() &&
                     ctrls_[(size_t)hover].kind == Ctrl::TableRow &&
                     ctrls_[(size_t)hover].a == (int)i;
    if (hov) {
      brush_->SetColor(thInk(0.06f));
      const D2D1_ROUNDED_RECT rr =
          D2D1::RoundedRect(D2D1::RectF(0.0f, y, cw, y + h), 6.0f, 6.0f);
      dc->FillRoundedRectangle(&rr, brush_.Get());
    }
    if (r.group) {
      brush_->SetColor(segColor(r.hsl, 1.0f));
      dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(7.5f, y + h * 0.5f), 3.5f, 3.5f),
                      brush_.Get());
      tabFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
      txt(r.name, tabFmt_.Get(), D2D1::RectF(16.0f, y, cw - 90.0f, y + h),
          thInk(0.90f));
      tabFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_CENTER);
      monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
      txt(wide(fmtExact(r.val)), monoFmt_.Get(),
          D2D1::RectF(cw - 92.0f, y, cw - 4.0f, y + h), thInk(0.60f));
      monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    } else {
      // 模型族色点：与提供商同色相的家族色（drilled），色即可辨不混入文字
      brush_->SetColor(segColor(r.hsl, 0.9f));
      dc->FillEllipse(D2D1::Ellipse(D2D1::Point2F(10.0f, y + h * 0.5f), 2.2f, 2.2f),
                      brush_.Get());
      txt(r.name, smallFmt_.Get(), D2D1::RectF(20.0f, y, cw - 90.0f, y + h),
          thInk(0.75f));
      monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
      txt(wide(fmtExact(r.val)), monoFmt_.Get(),
          D2D1::RectF(cw - 92.0f, y, cw - 4.0f, y + h), thMuted(1.0f));
      monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    }
    y += h;
  }
  dc->SetTransform(tm);
  dc->PopAxisAlignedClip();
  const int maxS = (std::max)(0, tableContentH_ - (int)(tableRc_.bottom - tableRc_.top));
  if (maxS > 0) {
    const float trackH = (tableRc_.bottom - tableRc_.top) - 8.0f;
    const float th = (std::max)(24.0f, trackH * (tableRc_.bottom - tableRc_.top) /
                                             (float)tableContentH_);
    const float ty = tableRc_.top + 4.0f + (trackH - th) * (float)scrollTable_ / (float)maxS;
    brush_->SetColor(thInk(0.25f));
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(
        D2D1::RectF(tableRc_.right - 6.0f, ty, tableRc_.right - 3.0f, ty + th),
        1.5f, 1.5f);
    dc->FillRoundedRectangle(&rr, brush_.Get());
  }
}

void OverviewPanel::drawActivity(render::D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  constexpr float gap = 2.0f;
  if (heat_.cells.empty()) {
    txt(L"当前窗口无数据", smallFmt_.Get(),
        D2D1::RectF(gridRc_.left + 10.0f, gridRc_.top + 24.0f, gridRc_.right - 10.0f,
                    gridRc_.top + 42.0f),
        thMuted(1.0f));
    return;
  }
  // 格阵裁剪（任何图形不出格阵区）
  dc->PushAxisAlignedClip(&gridRc_, D2D1_ANTIALIAS_MODE_PER_PRIMITIVE);
  // 星期行标（用户裁决 2026-09-17 截图：GitHub 同款 一/三/五/日 = 行 0/2/4/6）；
  // 每周粒度整列统一着色，行标无意义不画
  if (act_ != ActGrain::Week) {
    static const wchar_t* kRowLab[4] = {L"一", L"三", L"五", L"日"};
    smallFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
    for (int k = 0; k < 4; ++k) {
      const int r = 2 * k;
      const float y = gridY0_ + (float)r * (cell_ + gap);
      txt(kRowLab[k], smallFmt_.Get(),
          D2D1::RectF(gridX0_ - 17.0f, y, gridX0_ - 5.0f, y + cell_), thMuted(1.0f));
    }
    smallFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
  }
  // 格子：GitHub 式 7 行 × N 周列日历格阵（三档统一形态），色阶 5 档
  //（单色 accent 透明度递进，0 档用底板色；只读，hover 出 tooltip）
  for (size_t i = 0; i < heat_.cells.size(); ++i) {
    const int lv = heatLevel(heat_.cells[i].sums.total());
    brush_->SetColor(lv == 0 ? thInk(th().heat0) : thAccent(th().heatA[lv - 1]));
    if (act_ == ActGrain::Week) {
      // 每周：整周列 7 格统一按该周合计着色（本周列只画到今天）
      const int monday = heat_.cells[i].dayKey;
      for (int row = 0; row < 7; ++row) {
        if (addDaysK(monday, row) > todayKey_) break;
        const float x = gridX0_ + (float)i * (cell_ + gap);
        const float y = gridY0_ + (float)row * (cell_ + gap);
        const D2D1_ROUNDED_RECT rr =
            D2D1::RoundedRect(D2D1::RectF(x, y, x + cell_, y + cell_), 2.0f, 2.0f);
        dc->FillRoundedRectangle(&rr, brush_.Get());
        if ((int)i == hoverCell_ && row == hoverRow_) {
          brush_->SetColor(thInk(0.55f));
          dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
          brush_->SetColor(lv == 0 ? thInk(th().heat0) : thAccent(th().heatA[lv - 1]));
        }
      }
      continue;
    }
    const int col = (int)i / 7;
    const int row = (int)i % 7;
    const float x = gridX0_ + (float)col * (cell_ + gap);
    const float y = gridY0_ + (float)row * (cell_ + gap);
    const D2D1_ROUNDED_RECT rr =
        D2D1::RoundedRect(D2D1::RectF(x, y, x + cell_, y + cell_), 2.0f, 2.0f);
    dc->FillRoundedRectangle(&rr, brush_.Get());
    if ((int)i == hoverCell_) {  // 悬停格描边（tooltip 由 drawHeatTip 出）
      brush_->SetColor(thInk(0.55f));
      dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
    }
  }
  // 月份轴：月份首列（用周中日期定月份归属，原型同款）
  int lastM = -1;
  for (int col = 0; col < cols_; ++col) {
    const size_t i = act_ == ActGrain::Week ? (size_t)col : (size_t)col * 7;
    if (i >= heat_.cells.size()) break;
    const int m = (addDaysK(heat_.cells[i].dayKey, 3) / 100) % 100;
    if (m == lastM) continue;
    lastM = m;
    wchar_t buf[8];
    swprintf_s(buf, L"%d月", m);
    txt(buf, monoFmt_.Get(),
        D2D1::RectF(gridX0_ + (float)col * (cell_ + gap),
                    gridY0_ + (float)rows_ * (cell_ + gap) + 4.0f,
                    gridX0_ + (float)col * (cell_ + gap) + 40.0f,
                    gridY0_ + (float)rows_ * (cell_ + gap) + 16.0f),
        thMuted(1.0f));
  }
  dc->PopAxisAlignedClip();
}

void OverviewPanel::drawStats(render::D3DContext& d3d) {
  ID2D1DeviceContext* dc = d3d.dc();
  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  // 竖线分隔（格阵区 | 卡组区）
  brush_->SetColor(thInk(0.13f));
  const float sepX = statsRc_.left - 7.0f;
  dc->DrawLine(D2D1::Point2F(sepX, actRc_.top + 8.0f),
               D2D1::Point2F(sepX, actRc_.bottom - 8.0f), brush_.Get(), 1.0f);
  wchar_t dateBuf[16] = L"—";
  if (sum_.peakDayKey > 0)
    swprintf_s(dateBuf, L"%d月%d日", (sum_.peakDayKey / 100) % 100, sum_.peakDayKey % 100);
  const std::wstring cards[6][2] = {
      {L"累计 tokens", wide(fmtExact(sum_.totalTokens))},
      {L"累计轮数", wide(fmtExact(sum_.totalMsgs))},
      {L"活跃天数", std::to_wstring(sum_.activeDays) + L" 天"},
      {L"日均 tokens", wide(fmtExact(sum_.avgTokensPerActiveDay))},
      {sum_.peakDayKey > 0 ? L"峰值日 · " + std::wstring(dateBuf) : L"峰值日",
       sum_.peakDayKey > 0 ? wide(fmtExact(sum_.peakDayTokens)) : std::wstring(L"—")},
      {L"当前连续活跃", std::to_wstring(sum_.currentStreak) + L" 天"},
  };
  for (int i = 0; i < 6; ++i) {
    const bool hov = hover >= 0 && hover < (int)ctrls_.size() &&
                     ctrls_[(size_t)hover].kind == Ctrl::StatCard &&
                     ctrls_[(size_t)hover].a == i;
    brush_->SetColor(thInk(hov ? 0.06f : 0.03f));  // 只读：hover 轻微提亮，无点击
    const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(statRc_[i], 8.0f, 8.0f);
    dc->FillRoundedRectangle(&rr, brush_.Get());
    const D2D1_RECT_F& rc = statRc_[i];
    if (!statOneCol_) {  // 2 列：上标签下大数
      txt(cards[i][0], smallFmt_.Get(),
          D2D1::RectF(rc.left + 9.0f, rc.top + 5.0f, rc.right - 6.0f, rc.top + 21.0f),
          thMuted(1.0f));
      txt(cards[i][1], numFmt_.Get(),
          D2D1::RectF(rc.left + 9.0f, rc.top + 24.0f, rc.right - 6.0f, rc.bottom - 6.0f),
          thInk(0.92f));
    } else {  // 1 列：标签左、数值右同行
      txt(cards[i][0], smallFmt_.Get(),
          D2D1::RectF(rc.left + 9.0f, rc.top, rc.right - 90.0f, rc.bottom),
          thMuted(1.0f));
      monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_TRAILING);
      txt(cards[i][1], monoFmt_.Get(),
          D2D1::RectF(rc.right - 96.0f, rc.top, rc.right - 9.0f, rc.bottom),
          thInk(0.92f));
      monoFmt_->SetTextAlignment(DWRITE_TEXT_ALIGNMENT_LEADING);
    }
  }
}

void OverviewPanel::drawHeatTip(render::D3DContext& d3d, render::IMaterial& material) {
  if (hoverCell_ < 0 || hoverCell_ >= (int)heat_.cells.size()) return;
  ID2D1DeviceContext* dc = d3d.dc();
  const HeatCell& c = heat_.cells[(size_t)hoverCell_];
  wchar_t tbuf[64];
  if (act_ == ActGrain::Week) {  // YYYY年M月D日 · 当周（D日=鼠标所在格日期）
    swprintf_s(tbuf, L"%d年%d月%d日 · 当周", hoverDay_ / 10000, (hoverDay_ / 100) % 100,
               hoverDay_ % 100);
  } else if (act_ == ActGrain::Cumulative) {  // 截至 2026年9月17日
    swprintf_s(tbuf, L"截至 %d年%d月%d日", c.dayKey / 10000, (c.dayKey / 100) % 100,
               c.dayKey % 100);
  } else {  // 2026年9月15日
    swprintf_s(tbuf, L"%d年%d月%d日", c.dayKey / 10000, (c.dayKey / 100) % 100,
               c.dayKey % 100);
  }
  std::wstring line = act_ == ActGrain::Cumulative ? L"当周累计 " : L"";
  line += wide(fmtExact(c.sums.total())) + L" tokens";
  if (c.sums.msgs > 0)  // 轮数为 0（旧数据无计数）时省略"轮消息"段，不显示"0 轮"凑数
    line += L" · " + wide(fmtExact(c.sums.msgs)) + L" 轮消息";
  auto mwidth = [&](IDWriteTextFormat* fmt, const std::wstring& s) {
    ComPtr<IDWriteTextLayout> tl;
    if (SUCCEEDED(d3d.dwrite()->CreateTextLayout(s.c_str(), (UINT32)s.size(), fmt,
                                                 2000.0f, 100.0f, &tl))) {
      DWRITE_TEXT_METRICS m{};
      if (SUCCEEDED(tl->GetMetrics(&m))) return m.width;
    }
    return measure(fmt, s);
  };
  float cw = mwidth(tipFmt_.Get(), tbuf);
  cw = (std::max)(cw, mwidth(smallFmt_.Get(), line));
  const float cardW = cw + 22.0f;
  const float cardH = 10.0f + 18.0f + 17.0f + 8.0f;
  const float W = rect.right - rect.left;
  const float H = rect.bottom - rect.top;
  float x = heatPx_ + 14.0f, y = heatPy_ + 12.0f;  // 面板坐标，夹取面板内
  if (x + cardW > W - 8.0f) x = heatPx_ - cardW - 12.0f;
  if (x < 8.0f) x = 8.0f;
  if (y + cardH > H - 8.0f) y = heatPy_ - cardH - 10.0f;
  if (y < 8.0f) y = 8.0f;
  D2D1_MATRIX_3X2_F tm;
  dc->GetTransform(&tm);
  dc->SetTransform(D2D1::IdentityMatrix());
  const D2D1_RECT_F cardRc = D2D1::RectF(rect.left + x, rect.top + y,
                                          rect.left + x + cardW, rect.top + y + cardH);
  (void)material;  // tooltip 卡底走主题 --surface（与主图 tooltip 同款）
  brush_->SetColor(th().surface);
  const D2D1_ROUNDED_RECT crr = D2D1::RoundedRect(cardRc, 12.0f, 12.0f);
  dc->FillRoundedRectangle(&crr, brush_.Get());
  brush_->SetColor(thInk(0.13f));
  const D2D1_ROUNDED_RECT brr = D2D1::RoundedRect(cardRc, 12.0f, 12.0f);
  dc->DrawRoundedRectangle(&brr, brush_.Get(), 1.0f);
  dc->SetTransform(tm);
  auto txt = [&](const std::wstring& s, IDWriteTextFormat* fmt,
                 const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), fmt, &rc, brush_.Get());
  };
  txt(tbuf, tipFmt_.Get(), D2D1::RectF(x + 11.0f, y + 6.0f, x + cardW - 8.0f, y + 24.0f),
      thInk(0.92f));
  txt(line, smallFmt_.Get(), D2D1::RectF(x + 11.0f, y + 26.0f, x + cardW - 8.0f, y + 43.0f),
      thInk(0.80f));
}

void OverviewPanel::showToast(render::D3DContext& d3d, const std::wstring& text,
                              const std::wstring& path, int64_t nowMs) {
  toastText_ = text;
  toastPath_ = path;
  toastUntilMs_ = nowMs + 6000;  // ~6s 自动消失；再点新截图重置计时
  // 卡矩形现算（底注上方居中）：measure 需设备，失败则给估值
  float tw = 0, lw = 0;
  if (ensure(d3d)) {
    ComPtr<IDWriteTextLayout> tl;
    if (SUCCEEDED(d3d.dwrite()->CreateTextLayout(text.c_str(), (UINT32)text.size(),
                                                 smallFmt_.Get(), 2000.0f, 100.0f, &tl))) {
      DWRITE_TEXT_METRICS m{};
      if (SUCCEEDED(tl->GetMetrics(&m))) tw = m.width;
    }
    if (!path.empty()) {
      ComPtr<IDWriteTextLayout> tl2;
      if (SUCCEEDED(d3d.dwrite()->CreateTextLayout(L"打开文件夹", 5, smallFmt_.Get(),
                                                   2000.0f, 100.0f, &tl2))) {
        DWRITE_TEXT_METRICS m2{};
        if (SUCCEEDED(tl2->GetMetrics(&m2))) lw = m2.width;
      }
    }
  }
  if (tw <= 0) tw = (float)text.size() * 11.0f;
  if (lw <= 0 && !path.empty()) lw = 58.0f;
  const float cardW = tw + (path.empty() ? 0.0f : lw + 16.0f) + 24.0f;
  const float cardH = 32.0f;
  const float W = rect.right - rect.left;
  const float H = rect.bottom - rect.top;
  const float cx = (W - cardW) * 0.5f;
  const float cy = H - kFtH - 10.0f - cardH;
  toastRc_ = D2D1::RectF(cx, cy, cx + cardW, cy + cardH);
  toastLinkRc_ = path.empty() ? D2D1::RectF()
                              : D2D1::RectF(cx + cardW - 12.0f - lw - 6.0f, cy + 4.0f,
                                            cx + cardW - 8.0f, cy + cardH - 4.0f);
}

void OverviewPanel::drawToast(render::D3DContext& d3d) {
  if (toastUntilMs_ <= 0) return;
  ID2D1DeviceContext* dc = d3d.dc();
  brush_->SetColor(th().surface);
  const D2D1_ROUNDED_RECT rr = D2D1::RoundedRect(toastRc_, 8.0f, 8.0f);
  dc->FillRoundedRectangle(&rr, brush_.Get());
  brush_->SetColor(th().border);
  dc->DrawRoundedRectangle(&rr, brush_.Get(), 1.0f);
  auto txt = [&](const std::wstring& s, const D2D1_RECT_F& rc, D2D1_COLOR_F color) {
    if (s.empty()) return;
    brush_->SetColor(color);
    dc->DrawText(s.c_str(), (UINT32)s.size(), smallFmt_.Get(), &rc, brush_.Get());
  };
  const float linkX = toastPath_.empty() ? toastRc_.right : toastLinkRc_.left - 6.0f;
  txt(toastText_, D2D1::RectF(toastRc_.left + 12.0f, toastRc_.top, linkX, toastRc_.bottom),
      thInk(0.90f));
  if (!toastPath_.empty())
    txt(L"打开文件夹", toastLinkRc_,
        hover == kToastHit ? thAccent(1.0f) : thAccent(0.80f));
}

void OverviewPanel::drawOffscreen(render::D3DContext& d3d,
                                  render::IMaterial& material) {
  // 分享导出：rect 临时归零重放 draw（几何按原尺寸不变，调用方预设 2× 缩放变换）
  const D2D1_RECT_F saved = rect;
  rect = D2D1::RectF(0.0f, 0.0f, saved.right - saved.left, saved.bottom - saved.top);
  draw(d3d, material);
  rect = saved;
}

} // namespace okmeter
