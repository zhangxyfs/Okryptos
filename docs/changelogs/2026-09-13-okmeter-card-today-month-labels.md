# 2026-09-13 OkMeter：详情卡改版（模型卡今日置顶 + 新增本月口径 + 千分位原数 + 中英文标签）

## 需求（用户裁决）

1. 模型详情卡最上方大数字从累计改为**今日消耗**，下行依次 本周/**本月**/累计
   （新增本月口径）；
2. 总量卡当前会话区 input→输入、output→输出（拆分行 常规 input→常规输入）；
3. 详情卡全部 token 读数去掉小数点与万/亿单位，还原千分位逗号原数
   （"有单位看着还是更费劲"）。

## 改动

- `aggregator.{h,cpp}`：新增 `modelMonth(id, nowMs)`——byDay 键 yyyymmdd 直接
  /100 得月键求和，与 today/week 同模式；
- `app.cpp` rebuildCard：模型卡 big=`fmtExact(modelToday)`、rows=本周/本月/累计；
  行值与 big 统一 `fmtExact`（fmtYi 下卡）；总量卡标签中文化（输入/输出/
  常规输入，cache 行不变）；
- `settings.cpp` 开关说明文案同步（合并 cache 进输入、拆分行说明）；
- `test_aggregator.cpp` 新增 `agg_modelmonth_respects_month_boundary`（跨月
  边界：上月不计/本月两笔/未观测模型为 0）。

## 测试

- `build.bat` 编译 + 52 单测通过（含新增月口径用例），/W4 零警告。
- 在线注入 WM_MOUSEMOVE 验证：命中 item2（idx=1）→ hover 置位 → 卡片出现
  （诊断行已随验证完毕移除）。
