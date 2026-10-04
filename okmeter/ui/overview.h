// ui/overview.h —— 「用量总览」面板：工作区中央 80%×80% 玻璃面板（最小 640×480
// 夹取可视区），与设置面板互斥，同一套并集扩窗/holdOpen/ESC 沿检测/点击穿透机制。
// 主图（堆叠柱 ⇄ 趋势线，数据 core/bucket）+ 范围 tabs + 图例显隐/下钻 + 明细表 +
// 悬停 tooltip。帧模型 compute（refresh=纯数据帧）→ draw（按帧绘制）分离，为
// E 阶段平移/缩放/morph 留扩展口（参照原型 computeFrame→svgFor）。
// 视图状态（范围/图型/下钻/显隐）全部活内存，关窗即忘（不写盘）。
// 面板底/描边/文字色走材质（drawCardBack + glassfx），图表语义色走 chartcolors。
#pragma once

#include "../core/aggregator.h"
#include "../core/bucket.h"
#include "../render/d3d.h"
#include "../render/material.h"
#include "chartcolors.h"
#include <map>
#include <set>
#include <string>
#include <vector>

namespace okmeter {

class OverviewPanel {
public:
  struct Ctrl {
    enum Kind {
      CloseX,       // ✕（面板坐标，固定下标 0）
      RangeTab,     // 范围 tab：a = BucketRange 序（0日/1周/2月/3年）
      KindBtn,      // 图型分段钮：a = 0柱状/1趋势
      Crumb,        // 面包屑「‹ 返回提供商」（下钻时存在）
      LegendToggle, // 图例色块（显隐）：a = 分段下标
      LegendDrill,  // 图例名称/›（下钻）：a = 分段下标（仅提供商层）
      TableRow,     // 明细表行（悬停 dim，不可点）：a = 行下标；rc = 表内容坐标
      ActTab,       // 活动图粒度 tab：a = 0每日/1每周/2累计
      StatCard,     // 累计摘要卡（悬停提亮，不可点）：a = 卡下标 0..5
      ThemeBtn,     // 主题切换钮（太阳/月亮，标题栏 ✕ 左侧）
      ShareBtn,     // 分享钮（标题栏，主题钮左侧）：面板离屏渲染存 PNG
      PinBtn        // 置顶钮（标题栏，分享钮左侧）：dock 窗口置顶开/关
    } kind = CloseX;
    int a = 0;
    D2D1_RECT_F rc{};  // 面板坐标；Legend*/TableRow 为各自区内容坐标（命中 +scroll）
  };

  bool open = false;
  D2D1_RECT_F rect{};   // 面板矩形（窗口客户区坐标，place 填充）
  int hover = -1;        // 悬停控件下标（ctrls_）

  void begin(const Aggregator& agg);              // 底注 + 视图状态复位
  void refresh(const Aggregator& agg, int64_t nowMs);  // 纯数据帧重算（compute）
  void layout(render::D3DContext& d3d);           // 几何 + 控件表（draw 内脏时自动调）
  void place(float x, float y, float w, float h);
  void draw(render::D3DContext& d3d, render::IMaterial& material);
  int hit(int x, int y) const;       // 命中控件下标（窗口客户区坐标；-1=无/面板外）
  bool contains(int x, int y) const;
  // 悬停控件更新 + dim 分段推导（图例行/明细行 → 主图其余降暗；返回是否有变化）
  bool setHover(int ctrlIdx);
  // 主图悬停（面板坐标）：柱状=悬停桶列，趋势=最近桶中心参考线；返回是否有变化
  bool plotHoverAt(float px, float py);
  bool clearPlotHover();
  // 活动图悬停（面板坐标）：格子 tooltip；返回是否有变化
  bool heatHoverAt(float px, float py);
  bool clearHeatHover();
  // 主图 plot 区拖拽平移（像素级连续滑移，整桶边界换数据窗；阈值 <4px 视为悬停）
  bool panBegin(float px, float py);  // plot 空白处按下（动画中按下落终态再拖）
  bool panActive() const { return panArmed_; }
  bool panning() const { return panning_; }
  int panMove(float px);              // 0=无变化 1=重绘 2=数据窗变（需 refresh）
  void panEnd();
  // 滚轮（窗口客户区坐标）：0=无 1=右栏滚动（重绘）2=主图缩放（需 refresh+重排+重绘）
  int wheelAt(int x, int y, int delta);
  // 范围 morph / 图型淡化动画（app 在 click 后的 refresh+layout 之后调 startPendingAnim
  // 启动，animTick 逐帧 tickAnim 推进；帧驱动走 needsFrames 既有通路）
  bool animBusy() const { return animType_ != 0; }
  void startPendingAnim(int64_t nowMs);
  bool tickAnim(int64_t nowMs);  // 推进动画/toast 计时；返回 true = 需要紧跟一帧重绘
  int click(int idx);  // 0=无变化 1=需 refresh+layout+重绘 2=请求关闭 3=主题已切换（app 落盘）
                       // 4=请求导出 PNG 分享 5=toast「打开文件夹」6=请求切换窗口置顶
  // 面板双主题（黑/白，与 dock 材质解耦；token 照 prototype-overview-v2 两套 CSS 变量）
  void setTheme(const std::string& theme);  // "light"/"dark"
  const std::string& theme() const { return theme_; }
  // 窗口置顶态（仅图标显示用；实际 SetWindowPos 由 app 执行并回填）
  void setTopmost(bool on) { topmostOn_ = on; }
  bool topmost() const { return topmostOn_; }
  // 标题栏空白（面板坐标）：可拖拽移动面板的命中区（按钮除外）；app 起拖用
  bool titleBarHit(float px, float py) const;
  // 分享：面板内容离屏重放（app 经 D3DContext::renderOffscreen 调，rect 归零按当前
  // 主题/视图状态直绘；不含 dock）
  void drawOffscreen(render::D3DContext& d3d, render::IMaterial& material);
  void showToast(render::D3DContext& d3d, const std::wstring& text,
                 const std::wstring& path, int64_t nowMs);  // path 空 = 失败 toast
  bool toastActive() const { return toastUntilMs_ > 0; }
  const std::wstring& toastPath() const { return toastPath_; }

private:
  // 主题 token（v2 :root 浅色 / [data-theme='dark'] 深色，oklch→sRGB 已换算）
  struct OvTheme {
    D2D1_COLOR_F bg;       // 面板底（--surface）
    D2D1_COLOR_F surface;  // tooltip 卡底
    D2D1_COLOR_F fg;       // 主墨色（--fg）
    D2D1_COLOR_F muted;    // 次墨色（--muted）
    D2D1_COLOR_F border;   // 描边（--border）
    D2D1_COLOR_F accent;   // 主题 accent（tab 高亮/面包屑/热力色阶）
    float heatA[4];        // 热力 1–4 档 accent 透明度（0 档用 fg 6%/7%）
    float heat0;           // 热力 0 档 fg 透明度
  };
  const OvTheme& th() const { return th_; }
  D2D1_COLOR_F thInk(float a) const;     // th().fg + alpha
  D2D1_COLOR_F thMuted(float a) const;   // th().muted + alpha
  D2D1_COLOR_F thAccent(float a) const;  // th().accent + alpha
  enum class ChartKind { Bars, Trend };

  // ── 数据帧（refresh 填；E 阶段平移/缩放/morph 在此之上插值/变换）──
  struct Seg {
    std::string id, label;        // 提供商层 id=提供商名；下钻 id=模型全名 label=短名
    chartcolors::Hsl hsl;
    int64_t windowTotal = 0;      // 当前窗口合计（图例/排序用）
    bool hidden = false;
  };
  struct BucketF {
    int key = 0, beginKey = 0, endKey = 0;
    std::vector<int64_t> segVals;  // 与 segs_ 对齐（含隐藏段原值，绘制跳过）
  };
  struct TRow {                    // 明细表行（内容坐标，layout 布矩形）
    bool group = false;            // 组头（提供商）
    std::string segId;             // dim 目标（提供商层模型行 = 所属提供商）
    std::wstring name;
    int64_t val = 0;
    chartcolors::Hsl hsl;
  };

  bool ensure(render::D3DContext& d3d);
  float measure(IDWriteTextFormat* fmt, const std::wstring& s) const;
  static int64_t niceStep(int64_t raw);
  // ── 绘制帧（E：平移/缩放/morph 都以帧为单位绘制；vals 为 double 支持插值）──
  struct PlotFrame {
    std::vector<int> keys;     // 形态键（beginKey：日=当日、周=当周周一、月=当月1日、年=元旦）
    std::vector<float> xs;     // 桶中心 x（面板坐标，与 keys 对齐）
    std::vector<Seg> segs;     // 帧的分段清单（含 id/色板；vals 以此对齐——
                               // 两端分段清单不同的 morph 按键 id 对齐，缺失补 0，
                               // 防下标错位读出越界值——日⇄年红柱/趋势瞬移的根因）
    std::vector<std::vector<double>> vals;  // [k][seg]（对齐 segs）
    float bw = 0;              // 帧单桶像素宽（柱宽/悬停列宽换算）
    double yMax = 1.0;         // 帧 Y 轴最大（morph 双端插值，网格不瞬跳）
    BucketRange range = BucketRange::Day;  // 帧所属范围（刻度按各自范围格式化，morph 交叉淡化用）
  };
  PlotFrame buildPlotFrame() const;   // 从 buckets_ + 当前几何（含 panOffset_ 滑移）
  PlotFrame morphBars(const PlotFrame& o, const PlotFrame& n, double e) const;   // 键并集插值
  PlotFrame morphTrend(const PlotFrame& o, const PlotFrame& n, double e) const;  // 函数形状插值
  double plotYOf(double yMax, double v) const;
  float frameBucketX(size_t i) const;  // 桶 i 中心 x（panOffset_ 浮点滑移）
  std::wstring tickLabelForKey(int key, BucketRange range) const;
  std::wstring tipTitle(const BucketF& b) const;
  void drawPlot(render::D3DContext& d3d, const PlotFrame& f, ChartKind kind, float alpha);
  void drawXLabels(render::D3DContext& d3d, const PlotFrame& lf, float alpha);  // 刻度独立分层（morph 时新旧交叉淡化，防跳变闪帧）
  void drawBarsF(ID2D1DeviceContext* dc, const PlotFrame& f);
  void drawTrendF(ID2D1DeviceContext* dc, const PlotFrame& f);
  void stopAnim();                     // 直接操作（拖拽/缩放/显隐/下钻）立即终止动画
  void clampPan();
  void drawChart(render::D3DContext& d3d, render::IMaterial& material);
  void drawTooltip(render::D3DContext& d3d, render::IMaterial& material);
  void drawLegend(render::D3DContext& d3d);
  void drawTable(render::D3DContext& d3d);
  void drawActivity(render::D3DContext& d3d);          // Token 活动热力图 + 月份轴
  void drawStats(render::D3DContext& d3d);            // 累计摘要卡组（2×3，只读）
  void drawHeatTip(render::D3DContext& d3d, render::IMaterial& material);
  int heatLevel(int64_t v) const;                     // 色阶 0..4（P50/P75/P90 分档）
  static int addDaysK(int key, int n);                // dayKey ± n 天
  void frame(const D2D1_RECT_F& rc, const wchar_t* cap);  // 区域容器框 + 小标题
  ID2D1LinearGradientBrush* segGrad(const std::string& id, chartcolors::Hsl c,
                                    bool dim);             // 分段纵向渐变（按 id 缓存）
  const std::set<std::string>& hiddenSet() const;
  std::set<std::string>& hiddenSet();
  int visibleCount() const;

  static constexpr float kHdH = 46.0f;   // 标题栏高
  static constexpr float kTbH = 38.0f;   // 顶栏（tabs + 图型钮）高
  static constexpr float kFtH = 38.0f;   // 底注条高
  static constexpr float kPad = 14.0f;   // 面板内边距
  static constexpr float kGap = 10.0f;   // 区域间距
  static constexpr float kLegendRowH = 26.0f;
  static constexpr float kGroupRowH = 24.0f;
  static constexpr float kModelRowH = 20.0f;

  // ── 视图状态（活内存，关窗即忘；主题除外——setTheme 自 config 注入并持久化）──
  std::string theme_ = "light";
  OvTheme th_{};
  bool topmostOn_ = true;  // 置顶钮图标态（cfg_.topmost 回填）
  BucketRange range_ = BucketRange::Day;
  ChartKind kind_ = ChartKind::Bars;
  enum class ActGrain { Day, Week, Cumulative };  // 活动图粒度（拍板 5：累计=当周累计）
  ActGrain act_ = ActGrain::Day;
  std::string drill_;                                 // 空 = 提供商层
  std::set<std::string> hiddenProv_;                  // 隐藏提供商
  std::map<std::string, std::set<std::string>> hiddenModels_;  // 提供商 → 隐藏模型

  // ── 数据帧 ──
  std::vector<Seg> segs_;
  std::vector<BucketF> buckets_;
  std::vector<TRow> trows_;
  int64_t maxV_ = 0;          // 可见极值（柱=可见合计最大；趋势=单段最大）
  Heatmap heat_;              // 活动图格子（refresh 按 act_ 粒度现算）
  TotalsSummary sum_;         // 累计摘要 6 项（全跨度静态，不随粒度 tab 联动）
  int todayKey_ = 0;
  std::wstring foot_;

  // ── 几何（layout 填，面板坐标）──
  bool geomDirty_ = true;
  D2D1_RECT_F mainRc_{}, legendRc_{}, tableRc_{}, actRc_{};
  float plotX0_ = 0, plotY0_ = 0, plotW_ = 0, plotH_ = 0;  // plot 区（mainRc 内）
  float bw_ = 0, barW_ = 0;   // 单桶宽 / 柱宽
  int64_t yMax_ = 1, gridStep_ = 1;
  int tableContentH_ = 0, legendContentH_ = 0;
  int scrollTable_ = 0, scrollLegend_ = 0;
  int wheelResTable_ = 0, wheelResLegend_ = 0;  // 滚轮残差（settings 同款）
  std::vector<Ctrl> ctrls_;
  // 活动图几何
  D2D1_RECT_F gridRc_{}, statsRc_{};  // 格阵区 / 卡组区（actRc_ 内）
  float gridX0_ = 0, gridY0_ = 0, cell_ = 0;  // 格阵原点与格边长（gap=2）
  int cols_ = 0, rows_ = 0;                   // 周列数 / 行数（每日·累计=7，每周=1）
  D2D1_RECT_F statRc_[6]{};
  bool statOneCol_ = false;                   // 窄面板退化 1 列

  // ── 悬停 ──
  int hoverBucket_ = -1;
  float hoverPx_ = 0, hoverPy_ = 0;  // tooltip 锚点（面板坐标）
  std::string dimSeg_;               // 降暗例外分段（空 = 无 dim）
  int hoverCell_ = -1;               // 活动图悬停格（heat_.cells 下标）
  int hoverDay_ = 0;                 // 悬停格日期（每周粒度 tooltip 标题用）
  int hoverRow_ = 0;                 // 悬停格行下标（每周粒度列内描边用）
  float heatPx_ = 0, heatPy_ = 0;

  // ── toast（分享保存结果；~6s 自动消失，再点重置计时）──
  std::wstring toastText_, toastPath_;
  int64_t toastUntilMs_ = 0;
  D2D1_RECT_F toastRc_{}, toastLinkRc_{};
  void drawToast(render::D3DContext& d3d);
  static constexpr int kToastHit = 10000;  // hit 虚拟下标：toast「打开文件夹」

  // ── 平移/缩放（视图状态，不写盘；切范围 tab 归零）──
  double panOffset_ = 0;    // 向过去平移（桶单位，连续；0=锚定最近一桶）
  double maxPan_ = 0;       // 平移上限（数据起点夹取，refresh/layout 重算）
  int totalBuckets_ = 0;    // 当前范围 数据起点→当前桶 总桶数（refresh 填）
  int rangeCount_ = 7;      // 固定桶数（拍板 10：日7/周4/月12/年=跨度；refresh 填）
  bool bwCustom_ = false;   // 用户已滚轮缩放（false = 自适应恰好容纳 rangeCount_ 桶）
  bool panArmed_ = false;   // plot 按下待命（阈值内）
  bool panning_ = false;    // 拖拽进行中
  float panStartX_ = 0;
  double panStart_ = 0;

  // ── 动画（范围 morph 450ms / 图型淡化 150ms，easeDock 缓动）──
  int animType_ = 0;        // 0=无 1=morph 2=fade
  int pendingAnim_ = 0;     // click 置位，app refresh+layout 后 startPendingAnim 消费
  PlotFrame animFrom_;      // morph 旧帧 / fade 旧图型帧（接力时为当前合成帧）
  PlotFrame animCur_;       // morph 当前合成帧（tickAnim 推进）
  ChartKind animFromKind_ = ChartKind::Bars;
  int64_t animT0_ = 0;
  double animE_ = 1.0;

  // ── 设备资源（代际缓存，settings 同款）──
  ID2D1DeviceContext* seen_ = nullptr;
  unsigned seenGen_ = 0;
  Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brush_;
  Microsoft::WRL::ComPtr<ID2D1StrokeStyle> dashStyle_;  // 趋势参考线虚线
  struct GradPair {  // 分段纵向渐变：正常 / 降暗（alpha 0.18）
    Microsoft::WRL::ComPtr<ID2D1LinearGradientBrush> normal, dim;
  };
  std::map<std::string, GradPair> grads_;
  Microsoft::WRL::ComPtr<IDWriteTextFormat> titleFmt_;  // 标题 13 半粗
  Microsoft::WRL::ComPtr<IDWriteTextFormat> enFmt_;     // OVERVIEW 9 右对齐
  Microsoft::WRL::ComPtr<IDWriteTextFormat> tabFmt_;    // tabs/图型钮 11.5 中等
  Microsoft::WRL::ComPtr<IDWriteTextFormat> bodyFmt_;   // 图例名 12.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> smallFmt_;  // 说明/模型行 10.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> monoFmt_;   // 刻度/数值 Consolas 10.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> tipFmt_;    // tooltip 标题 11.5 半粗
  Microsoft::WRL::ComPtr<IDWriteTextFormat> footFmt_;   // 底注 10.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> numFmt_;    // 统计卡大数 Consolas 15 中等
};

} // namespace okmeter
