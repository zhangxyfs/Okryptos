// ui/settings.h —— 背板式设置面板：停靠在 dock 对侧屏幕边缘的玻璃面板
//（宽 328、顶标题栏 + 滚动体区 + 底部操作条），与 dock 同一 swapchain 扩窗绘制
//（参照 ui/menu 的区扩展模式）。控件全自绘（对齐原型 okmeter-dock-prototype.html
// 设置面板：缩略图选项卡 choice / chips / 自绘玻璃下拉 gsel+drop / 玻璃开关 sw）。
// 两段式保存：控件只改 draft（Config 副本），保存并生效由 app 落盘 + 全量 rebuild；
// 取消/✕/Escape 丢弃。面板底委托 material.drawCardBack（跟随当前生效材质供皮，
// 草稿态切材质不即时换肤）。下拉浮层面板内自绘（滚动即关，原型 closeDrop 同款）。
#pragma once

#include "../core/config.h"
#include "../render/d3d.h"
#include "../render/material.h"
#include <string>
#include <utility>
#include <vector>

namespace okmeter {

constexpr int kSettingsPanelW = 328;  // 面板宽（原型 .panel width）

class SettingsPanel {
public:
  struct Ctrl {
    enum Kind {
      CloseX, CancelBtn, SaveBtn,                       // 头/尾按钮（面板坐标，前三个固定）
      FormTab, MaterialTab, CountChip, EdgeChip,        // 选项卡/chips（体区内容坐标）
      MergeSwitch, Gsel,                                // 开关/下拉钮（体区内容坐标）
      DropItem                                          // 下拉浮层项（面板坐标）
    } kind = FormTab;
    int a = 0, b = 0;   // 选项卡/chips: a=选项下标；Gsel: a=槽位；DropItem: a=槽位 b=选项下标
    D2D1_RECT_F rc{};
    bool body = true;   // true=体区内容坐标（命中时 +scrollY）；false=面板坐标
  };

  bool open = false;
  Config draft;
  D2D1_RECT_F rect{};   // 面板矩形（窗口客户区坐标，place 填充）
  int scrollY = 0;      // 体区滚动偏移
  int hover = -1;       // 悬停控件下标（ctrls_）

  void begin(const Config& cur, std::vector<std::string> models);
  void layout(render::D3DContext& d3d);  // 文本测量 → 控件矩形/内容高度（count/模型变化后重调）
  void place(float x, float y, float h); // 面板落窗（窗口客户区坐标；头尾控件矩形同步）
  void draw(render::D3DContext& d3d, render::IMaterial& material);
  int hit(int x, int y) const;           // 命中控件下标（窗口客户区坐标；-1=无/面板外）
  bool contains(int x, int y) const;     // 点在面板矩形或打开的下拉浮层内
  bool dropOpen() const { return dropGsel_ >= 0; }
  int dropOwner() const { return dropGsel_; }
  void closeDrop();                      // 关下拉（移除 DropItem 控件）
  // 滚轮：浮层上滚浮层、体区上滚体区（体区滚动即关浮层，原型同款）；返回是否有变化
  bool wheelAt(int x, int y, int delta);
  int gselCtrl(int slot) const;          // 槽位 slot 的 Gsel 控件下标（-1=无；自检截图用）
  // 控件激活（仅改 draft / 开合下拉；CloseX/Cancel/Save 由 app 处理）。
  // 返回 0=无变化 1=需重绘 2=需重排+重绘
  int click(render::D3DContext& d3d, int idx);
  const Ctrl& ctrl(int idx) const { return ctrls_[(size_t)idx]; }

private:
  struct Sec { std::wstring title; float y0 = 0, y1 = 0, titleW = 0; };  // 节（内容坐标）

  bool ensure(render::D3DContext& d3d);
  void openDrop(render::D3DContext& d3d, int gselCtrl);  // 打开下拉（追加 DropItem 控件）
  int maxScroll() const;
  float bodyH() const { return (rect.bottom - rect.top) - kHdH - kFtH; }
  std::vector<std::pair<std::string, std::wstring>> optionsFor(int slot) const;
  std::wstring valueLabel(const std::string& v) const;
  // 缩略图矢量（原型 .choice svg 同款，100×44 视口等比缩放居中）
  void drawThumb(ID2D1DeviceContext* dc, int kind, int idx,
                 const D2D1_RECT_F& box, D2D1_COLOR_F color) const;
  // 省略号裁剪文本（CreateTextLayout trimming；gsel 当前值/下拉项/长模型 id 用）
  void drawTextTrimmed(render::D3DContext& d3d, const std::wstring& s,
                       IDWriteTextFormat* fmt, const D2D1_RECT_F& rc,
                       D2D1_COLOR_F color) const;

  static constexpr float kHdH = 46.0f;   // 标题栏高（p-hd padding 13+11 + 内容 ~20）
  static constexpr float kFtH = 54.0f;   // 底部操作条高（p-ft padding 10×2 + 按钮 33）
  static constexpr float kPadX = 15.0f;  // 体区左右 padding（原型 .p-bd padding 15）

  std::vector<std::string> models_;      // 已观测模型（下拉"模型 · id"枚举）
  std::vector<Ctrl> ctrls_;
  std::vector<Sec> secs_;                // 节标题/分隔（内容坐标）
  std::vector<std::pair<std::wstring, D2D1_RECT_F>> posLabels_;  // 映射行位置标签
  D2D1_RECT_F swText_{}, swSmall_{};     // 开关行文案（内容坐标）
  int contentH_ = 0;                     // 体区内容总高（layout 填充）
  int dropGsel_ = -1;                    // 打开的下拉所属 ctrls_ 下标（-1=无）
  size_t dropStart_ = 0;                 // DropItem 控件在 ctrls_ 的起始下标
  int dropScroll_ = 0;                   // 下拉浮层内部滚动
  D2D1_RECT_F dropRc_{};                 // 下拉浮层矩形（面板坐标）
  float saveBtnW_ = 100.0f, cancelBtnW_ = 72.0f;  // layout 测量，place 布矩形

  ID2D1DeviceContext* seen_ = nullptr;
  unsigned seenGen_ = 0;  // 设备代际（防 dc 地址复用 ABA 误判，DockScene::ensure 同款）
  Microsoft::WRL::ComPtr<ID2D1SolidColorBrush> brush_;
  Microsoft::WRL::ComPtr<ID2D1StrokeStyle> dashStyle_;   // 虚线（罗盘轨道/毛玻璃缩略图）
  Microsoft::WRL::ComPtr<IDWriteTextFormat> titleFmt_;   // 标题 Segoe UI 13 半粗
  Microsoft::WRL::ComPtr<IDWriteTextFormat> enFmt_;      // SETTINGS Segoe UI 9 右对齐
  Microsoft::WRL::ComPtr<IDWriteTextFormat> h4Fmt_;      // 节标题 Segoe UI 10.5 半粗
  Microsoft::WRL::ComPtr<IDWriteTextFormat> nameFmt_;    // 选项卡名 Segoe UI 12 居中
  Microsoft::WRL::ComPtr<IDWriteTextFormat> smallFmt_;   // 选项卡副标/备注 Segoe UI 10.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> bodyFmt_;    // chips/下拉 Segoe UI 12.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> posFmt_;     // 位置标签 Consolas 11.5
  Microsoft::WRL::ComPtr<IDWriteTextFormat> btnFmt_;     // 按钮 Segoe UI 12 居中
};

} // namespace okmeter
