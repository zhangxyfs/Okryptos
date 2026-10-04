# Windows.Graphics.Capture (WGC) 黑色背景问题排查与修复指南

## 1. 问题概述

在使用 Windows.Graphics.Capture (WGC) API 进行屏幕/窗口捕获并渲染时，输出画面背景呈现纯黑色，而非预期的透明或下层内容。

### 根本原因

WGC 输出的像素格式为 **BGRA8 Premultiplied Alpha（预乘 Alpha）**：

```
存储值: R' = R × A,  G' = G × A,  B' = B × A,  A = A
```

当渲染管线、SwapChain 配置或窗口样式未正确匹配该格式时，Alpha 通道被错误处理，导致透明区域显示为黑色。

---

## 2. 常见触发场景

| 场景 | 原因说明 |
| :--- | :--- |
| 直接 Blit 到不透明窗口 | Alpha 被忽略；预乘 RGB 本身偏暗；透明区域 `(0,0,0,A)` 显示为黑色 |
| Shader 中未做 Un-premultiply | 采样后直接输出，透明区域 RGB≈0 → 黑色 |
| SwapChain AlphaMode 错误 | 设为 `DXGI_ALPHA_MODE_IGNORE`，DWM 将 Alpha 视为 1.0 |
| 窗口缺少必要扩展样式 | 未设置 `WS_EX_NOREDIRECTIONBITMAP`，DWM 使用黑色重定向表面作底色 |
| Clear Color Alpha 为 1.0 | RenderTarget 清除色不透明，透明区域露出黑色底色 |
| WGC 边框捕获 | 窗口 DWM 装饰区域被包含，填充为黑色 |

---

## 3. 解决方案

### 3.1 正确配置 SwapChain Alpha 模式

这是最常见的遗漏项。必须确保 SwapChain 的 Alpha 模式与 WGC 输出格式匹配。

```cpp
DXGI_SWAPCHAIN_DESC1 desc = {};
desc.Width       = width;
desc.Height      = height;
desc.Format      = DXGI_FORMAT_B8G8R8A8_UNORM;
desc.BufferCount = 2;
desc.SwapEffect  = DXGI_SWAP_EFFECT_FLIP_DISCARD;

// ★ 关键：匹配 WGC 的预乘 Alpha 输出
desc.AlphaMode = DXGI_ALPHA_MODE_PREMULTIPLIED;

// 如果后续在 Shader 中做了 un-premultiply，则使用：
// desc.AlphaMode = DXGI_ALPHA_MODE_STRAIGHT;

IDXGISwapChain3* swapChain = nullptr;
HRESULT hr = factory->CreateSwapChainForComposition(device, &desc, nullptr, &swapChain);
```

> **注意**：`DXGI_ALPHA_MODE_PREMULTIPLIED` 告知 DWM 该 SwapChain 内容已是预乘格式，请直接合成。若设为 `DXGI_ALPHA_MODE_IGNORE`，DWM 会将所有像素 Alpha 当作 1.0 处理，透明区变黑。

### 3.2 Shader 中还原 Straight Alpha

如果渲染目标需要 Straight Alpha（例如后续还需与其他图层混合），需在 Pixel Shader 中反预乘：

```hlsl
Texture2D    tex    : register(t0);
SamplerState sampler : register(s0);

float4 PS_Main(float2 uv : TEXCOORD) : SV_TARGET
{
    float4 color = tex.Sample(sampler, uv);

    // ★ 反预乘：还原真实 RGB
    if (color.a > 0.001f)
    {
        color.rgb /= color.a;
    }
    else
    {
        color = float4(0.0f, 0.0f, 0.0f, 0.0f); // 完全透明区域归零
    }

    return color;
}
```

使用此 Shader 时，SwapChain 应配置为 `DXGI_ALPHA_MODE_STRAIGHT`。

### 3.3 确保窗口支持透明合成

#### 推荐方式：DWM 直接合成

```cpp
// WS_EX_NOREDIRECTIONBITMAP 告知 DWM 该窗口无传统 GDI 重定向表面，
// 内容由 SwapChain / DirectComposition 直接提供。
// 缺少此标志时，DWM 会以黑色重定向表面作为底色。
SetWindowLongPtr(hwnd, GWL_EXSTYLE,
    GetWindowLongPtr(hwnd, GWL_EXSTYLE) | WS_EX_NOREDIRECTIONBITMAP);
```

#### 传统方式：分层窗口

```cpp
// 适用于不使用 SwapChain、通过 UpdateLayeredWindow 手动提交的场景
SetWindowLongPtr(hwnd, GWL_EXSTYLE,
    GetWindowLongPtr(hwnd, GWL_EXSTYLE) | WS_EX_LAYERED | WS_EX_NOREDIRECTIONBITMAP);
```

> **核心要点**：无论哪种方式，`WS_EX_NOREDIRECTIONBITMAP` 都是消除黑色底色的必要条件。

### 3.4 检查 RenderTarget Clear Color

```cpp
// ❌ 错误：透明区域显示为黑色
float clearColor[] = { 0.0f, 0.0f, 0.0f, 1.0f };

// ✅ 正确：透明区域真正透明
float clearColor[] = { 0.0f, 0.0f, 0.0f, 0.0f };

context->ClearRenderTargetView(rtv, clearColor);
```

### 3.5 消除 WGC 捕获的黑色边框

WGC 可能捕获窗口外围的 DWM 阴影/装饰区域，表现为 1~2px 黑色边框。

#### Windows 11+ 禁用边框捕获

```cpp
// Windows 11 Build 22000+
if (winrt::Windows::Foundation::Metadata::ApiInformation::IsPropertyPresent(
        L"Windows.Graphics.Capture.GraphicsCaptureSession",
        L"IsBorderRequired"))
{
    session.IsBorderRequired(false);
}
```

#### Shader 内缩裁剪（兼容旧系统）

```hlsl
// 在 VS 或 PS 中将 UV 向内收缩 1px，避开边缘黑色像素
float2 safeUV = uv;
float borderPx = 1.0f;
safeUV.x = clamp(uv.x, borderPx / texWidth,  1.0f - borderPx / texWidth);
safeUV.y = clamp(uv.y, borderPx / texHeight, 1.0f - borderPx / texHeight);
float4 color = tex.Sample(sampler, safeUV);
```

---

## 4. 快速诊断清单

按优先级逐项检查，通常前 3 项即可定位问题：

- [ ] SwapChain `AlphaMode` 是否为 `PREMULTIPLIED` 或 `STRAIGHT`（不能是 `IGNORE`）
- [ ] 窗口是否设置了 `WS_EX_NOREDIRECTIONBITMAP` 扩展样式
- [ ] RenderTarget Clear Color 的 Alpha 分量是否为 `0.0f`
- [ ] Pixel Shader 是否正确处理了预乘 Alpha（直出 PREMULTIPLIED 或 un-premultiply 后 STRAIGHT）
- [ ] WGC Session 是否设置了 `IsBorderRequired(false)`（Windows 11+）
- [ ] `FrameArrived` 回调中获取 Surface 时是否发生了额外拷贝导致 Alpha 丢失
- [ ] 多显示器 / DPI 切换后是否重新创建了 SwapChain（旧 SwapChain 可能保留了错误的 AlphaMode）

---

## 5. Alpha 模式选择决策树

```
WGC 帧到达
    │
    ├─ 是否需要中间合成/后处理？
    │     ├─ 否 → SwapChain: PREMULTIPLIED + Shader 直出 + Clear(0,0,0,0)
    │     └─ 是 → 需要 Straight Alpha 参与混合运算
    │               ├─ Shader 中 un-premultiply → SwapChain: STRAIGHT
    │               └─ 保持预乘做混合 → SwapChain: PREMULTIPLIED
    │
    └─ 窗口样式
          └─ 必须包含 WS_EX_NOREDIRECTIONBITMAP
```

---

## 6. 参考资料

-   [Microsoft Docs: GraphicsCaptureSession.IsBorderRequired](https://learn.microsoft.com/en-us/uwp/api/windows.graphics.capture.graphicscapturesession.isborderrequired)
-   [Microsoft Docs: DXGI_ALPHA_MODE enumeration](https://learn.microsoft.com/en-us/windows/win32/api/dxgi1_2/ne-dxgi1_2-dxgi_alpha_mode)
-   [Microsoft Docs: WS_EX_NOREDIRECTIONBITMAP](https://learn.microsoft.com/en-us/windows/win32/winmsg/extended-window-styles)
-   [Microsoft Docs: Screen Capture with Windows.Graphics.Capture](https://learn.microsoft.com/en-us/windows/uwp/audio-video-camera/screen-capture)
-   [DirectComposition + WGC 高性能透明窗口示例](https://github.com/microsoft/Windows-classic-samples/tree/main/Samples/DirectComposition)