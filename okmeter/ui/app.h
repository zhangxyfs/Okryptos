// ui/app.h —— Dock 窗口应用：透明无边框置顶窗口 + 三 timer 数据接线 + 两态弹簧
#pragma once

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "../core/binding.h"
#include "../core/config.h"
#include "../core/spring.h"
#include "../render/d3d.h"
#include "../render/dock_scene.h"
#include "../render/backdrop.h"
#include "geometry.h"
#include "menu.h"
#include "settings.h"
#include "watch.h"
#include <cstdint>
#include <unordered_map>
#include <memory>
#include <vector>

namespace okmeter {

class Store;
class KimiAdapter;

// 单线程契约：poll 与渲染读都在 UI 线程，由定时器串行驱动（aggregator.h 约定）。
// 动画时钟为高分辨率可等待定时器 16ms（MsgWaitForMultipleObjectsEx 消息循环；
// 不支持时回退 16ms WM_TIMER），按需驱动（needsFrames 集中判定，syncFrames 在状态
// 翻转沿启/停）：弹簧 step（真实 elapsed dt）+ 需要时重绘 + SetWindowPos + 每 500ms
// 检查 RDCW 目录监听 → 触发则提前 poll。静止期（无任何自变元素且无进行中动画）
// 帧时钟整体停摆，RDCW 完成事件仍在等待集里可直接唤醒 poll；鼠标/定时器事件单帧
// 渲染后按需再评估。数据刷新：RDCW 事件即时 poll；2s WM_TIMER 在监听健康时退避为
// 30s 兜底（防 RDCW 缓冲溢出静默丢事件；全量递归枚举实测 ~180ms/次，1095 个
// wire.jsonl 下 2s 常轮是静置 CPU 主源），监听失效则保持 2s 纯轮询（原行为）。
// 另有 30s WM_TIMER 刷新相对时间文本；离开迟滞 600ms 一次性 WM_TIMER。
// 右键菜单：自绘玻璃菜单（ui/menu.*，原生 TrackPopupMenu 已废除——其模态泵会冻结
// 动画且观感不达标）：球上右键出映射子项组（改 mapping → saveConfig → rebuild 立即
// 生效）+ 设置…（打开背板设置面板）+ 换边（edge 互换 + saveConfig + 重建几何）+
// 退出（flush 游标）；弧线/空白右键仅后三项。菜单区纳入 dock 窗口（并集扩窗，
// 球区偏移记 zoneDX_/zoneDY_），菜单外点击/Escape 收起。
// 设置面板：背板式（ui/settings.*，规格 §3.5）——停靠 dock 对侧屏缘的 328px 玻璃
// 面板（标题栏 + 滚动体区 + 底部操作条），与菜单同一套并集扩窗/同渲染目标绘制。
// 两段式保存：控件只改 draft，保存并生效 → normalize + saveConfig + createModules
// 全量 rebuild；取消/✕/Escape 丢弃。面板打开期间 dock 保持展开；联合窗口横贯全屏，
// WM_NCHITTEST 对中部空白区回 HTTRANSPARENT 穿透；滚轮走原始输入（RIDEV_INPUTSINK，
// NOACTIVATE 窗口收不到 WM_MOUSEWHEEL）。
// 拖拽换边（规格 §3.2）：WM_LBUTTONDOWN 起拖（SetCapture，阈值内视为按压/点击），
// 拖动实时跟随；松手按窗口中心所在屏的工作区中线判定左/右缘，换边则 saveConfig。
// 多显示器：几何/定位一律按"窗口中心所在屏"的 MONITORINFO.rcWork（workArea()），
// WM_DPICHANGED/WM_DISPLAYCHANGE 重建布局。
// 两态：emerge 弹簧 e∈[0,1]，右缘窗口 x = 工作区右 - lerp(24, g.w, e) - (wide?268:0)；
// 展开态窗口宽 g.w+268（卡区在球区屏内侧，右缘靠左），收缩态宽 g.w；
// 鼠标进入 e→1，离开 600ms 迟滞后 e→0。
class DockApp {
public:
  ~DockApp();
  // shotPath 非空 = 自检截图模式：启动后强制展开中心球，2.5s 后存 PNG 退出；
  // shotMenuSlot != -2 时截图前打开自绘菜单（-1=空白菜单，≥0=该槽位球菜单）；
  // shotSettings=true 时截图前打开背板设置面板（180ms 滑入播完后落盘）；
  // shotDropSlot ≥0 时同时打开该槽位的指标下拉浮层
  int run(HINSTANCE inst, const std::wstring& shotPath = L"", int shotMenuSlot = -2,
          bool shotSettings = false, int shotDropSlot = -1,
          bool shotCollapsed = false);

private:
  static LRESULT CALLBACK WndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp);
  LRESULT onMessage(UINT msg, WPARAM wp, LPARAM lp);        // 分发 + syncFrames 收尾
  LRESULT dispatchMessage(UINT msg, WPARAM wp, LPARAM lp);  // 各消息实际处理

  void render();          // 掉帧补呈包装：renderOnce 触发设备重建丢帧时有界重试（防黑窗滞留）
  void renderOnce();      // 布局 → applyHover → dock_scene 一帧（形态/材质委托，含详情卡）
  void animTick();        // 动画帧：RDCW 检查 + 形态 tick + 弹簧 step（真实 dt）+ 挪窗 + 重绘
  // 按需渲染（静止帧 CPU≈0）：needsFrames 集中判定"是否需要持续帧"——弹簧未稳 /
  // 材质自主动画（glow 粒子气态漂移）/ 形态自主动画（罗盘收缩态旋转）/ 菜单/面板
  // 打开（滑入动画 + Escape·窗外点击沿检测在 animTick）/ 详情卡 cardin 未播完；
  // syncFrames 在状态翻转沿启/停帧时钟（HR 定时器 Cancel/重武装，回退路 SetTimer/
  // KillTimer），静止期事件（鼠标/滚轮/30s 刷新/RDCW 唤醒/配置变更）单帧渲染
  bool needsFrames() const;
  void syncFrames();
  void startFrames();
  void stopFrames();
  void pollData();        // kimi.poll→agg.add→flush→rebuildItems→重绘
  void rebuildItems();    // resolveBindings + 文本缓存（值/短名/占比）+ 详情卡重组
  void rebuildCard();     // 按 hoverIdx 组装详情卡（hover 变化/数据刷新时调用）
  void rebuildLayout();   // 工作区/边 → dockW_/winH_/winY_ 重算 + applyWindowPos
  // 统一窗口矩形：基础（弹簧 e + 卡区 wide）∪ 菜单屏幕矩形（菜单打开时）；
  // zoneDX_/zoneDY_ = 球区在窗口内的偏移（卡区/菜单区让位），SetWindowPos 落窗
  void applyWindowPos();
  void syncClickThru();  // 设置面板期按指针位置动态开关整窗 WS_EX_TRANSPARENT
  void updatePosition() { applyWindowPos(); }
  void setEmergeTarget(double t);
  void setWide(bool w);   // 展开态窗口宽 g.w+268（卡区）；收缩态回 g.w
  void flipEdge();        // 换边：edge 互换 → saveConfig → 重建位置几何
  void exitApp();         // 退出：flush 游标落盘 → DestroyWindow
  void startCapture();    // 背景捕获：取当前 DXGI 设备 → backdrop_.start（可重入）
  void createModules();   // 按 cfg_.form/material 从模块目录创建形态/材质（未知回退 arc/dark）
  RECT workArea() const;  // 窗口中心所在屏的 MONITORINFO.rcWork（无窗口/失败回退主屏）
  // 自绘玻璃右键菜单：组装内容（映射组当前值 ✓）→ 测量 → 屏缘内侧定位（不出屏）
  // → 并集扩窗；收起即收回基础矩形，指针已在窗外则恢复 600ms 迟滞
  void buildMenuEntries(int slot);
  void openSub1(int parentIdx, int subKind);           // 二级：总量四口径/模型厂商分组
  void openSub2(int parentIdx, const std::string& vendor);  // 三级：厂商下具体模型
  void placeSubColumns();                              // 子列定位（屏内侧逐级展开+并集矩形）
  void openMenu(int clientX, int clientY, int slot);
  void closeMenu();
  void activateMenu(int idx);
  void applyMenuMapping(const std::string& v);  // 叶项映射落盘（三级菜单共用）
  double menuAnimT() const;  // 弹出动画进度（0..1，ease-dock 缓动）
  // 背板设置面板：打开（draft=cfg 副本 + 模型枚举 + 对侧屏缘定位 + 并集扩窗）/
  // 关闭（apply=true → normalize+saveConfig+createModules 全量 rebuild；false 丢弃）
  void openSettings();
  void closeSettings(bool apply);
  void activateSettings(int idx);  // 面板控件命中分发（按钮/选项卡/chips/下拉/开关）
  double panelAnimT() const;       // 面板滑入动画进度（180ms，原型 panelin）
  double cardAnimT() const;        // 详情卡 cardin 出现动画进度（140ms，原型 .detail）
  // 悬停/按压命中：烘焙坐标（tuck+dy+卡区偏移）下的 2D 归一化距离 ≤1 最近项
  int hitItem(const DockGeom& g, int mx, int my, float dx) const;

  render::D3DContext d3d_;
  render::DockScene scene_;
  render::BackdropCapture backdrop_;  // WGC 实时背景捕获（降级链见 backdrop.h）
  unsigned backdropGen_ = 0;          // 上次接线捕获时的 D3D 代际（设备丢失重建检测）
  std::unique_ptr<render::IForm> form_;        // 形态（cfg.form，注册表创建）
  std::unique_ptr<render::IMaterial> material_;  // 材质（cfg.material，注册表创建）
  bool sessionNotif_ = false;         // WTS 会话通知已注册
  HWND hwnd_ = nullptr;
  DirWatcher watch_;          // sessions 目录 RDCW 监听（start 失败则纯轮询）

  std::unique_ptr<Store> store_;
  std::unique_ptr<KimiAdapter> kimi_;
  Config cfg_;
  std::vector<Binding> bindings_;
  std::vector<render::DockItem> items_;
  std::unordered_map<std::string, std::vector<double>> histByKey_;  // wave 历史（绑定键 → 26 点增量）
  std::unordered_map<std::string, int64_t> rawByKey_;               // wave 上次累计（算增量用）

  Spring emerge_;
  double emergeTarget_ = 0;
  bool emerged_ = true;       // 弹簧已静止在 target（避免静止帧空转）
  int hoverIdx_ = -1;
  int pressIdx_ = -1;           // 左键按住项（沉浸光感按压下沉/光晕；仅 glow 生效）
  bool trackingLeave_ = false;
  bool dragArmed_ = false;      // 左键已按下未越阈值（拖拽预备；阈值内=按压/点击）
  bool dragging_ = false;       // 拖拽进行中（窗口实时跟随，松手按半屏判定换边）
  POINT dragGrab_{};            // 起拖时指针相对窗口左上角的偏移（屏幕坐标系）
  POINT dragStart_{};           // 起拖指针屏幕坐标（拖拽阈值判定）
  bool wide_ = false;         // 窗口含 268px 卡区（展开态）
  render::DetailCard card_;   // 悬停详情卡缓存（rebuildCard 重组）
  GlassMenu menu_;            // 自绘右键菜单（open 时窗口并集扩出菜单区）
  RECT menuScreen_{};         // 菜单屏幕矩形（打开时定位，扩窗/夹取基准）
  LARGE_INTEGER menuOpenQpc_{};  // 菜单打开时刻（120ms 弹出动画计时）
  SettingsPanel settings_;    // 背板设置面板（open 时窗口并集扩出面板区）
  RECT panelScreen_{};        // 面板屏幕矩形（dock 对侧屏缘，打开时定位）
  LARGE_INTEGER panelOpenQpc_{}; // 面板打开时刻（180ms 滑入动画计时）
  LARGE_INTEGER cardShownQpc_{}; // 详情卡出现时刻（cardin 140ms 出现动画计时）
  int zoneDX_ = 0;            // 球区在窗口内的 x 偏移（卡区 268/菜单区让位）
  int zoneDY_ = 0;            // 球区 y 偏移（菜单向上扩窗时 >0）
  bool prevEsc_ = false;      // 上一动画帧 Escape 状态（菜单收起沿检测）
  bool clickThru_ = false;    // 设置面板期整窗 WS_EX_TRANSPARENT 状态（syncClickThru）
  bool prevLmb_ = false;      // 上一帧左键状态（菜单外点击收起沿检测）
  bool prevRmb_ = false;      // 上一帧右键状态（同上）
  int shotMenuSlot_ = -2;     // --shotmenu 自检：截图前打开的菜单槽位（-2=不开）
  bool shotSettings_ = false;  // --shotsettings 自检：截图前打开设置面板
  bool shotCollapsed_ = false; // --shotcap 自检：收缩态（e=0 露出条）截图
  int shotDropSlot_ = -1;      // --shotsettings 自检：同时打开的下拉槽位（-1=不开）
  int winY_ = 0;              // 垂直居中 y（rebuildLayout 重算）
  int winH_ = 0;              // 窗口高（卡垂直夹取/宽度切换用）
  int dockW_ = 150;           // layoutArc g.w（位置插值用）
  std::wstring shotPath_;     // --shot 自检截图输出路径（空=正常模式）
  int64_t lastWatchMs_ = 0;   // 上次 RDCW 检查时刻（动画帧里每 500ms 一次）
  LARGE_INTEGER lastTickQpc_{};  // 上一动画 tick 的 QPC（真实 dt 采样点，含静止 tick）
  HANDLE animTimer_ = nullptr;  // HR 可等待定时器动画时钟（NULL → WM_TIMER 回退）
  bool framesOn_ = false;       // 帧时钟运行中（syncFrames 按需启/停；静止期停摆）
  bool watchActive_ = false;    // RDCW 监听已建立（健康时 2s 轮询退避为 30s 兜底）
  int64_t lastPollMs_ = 0;      // 上次 poll 时刻（退避判定；RDCW 触发的 poll 同样刷新）
};

} // namespace okmeter
