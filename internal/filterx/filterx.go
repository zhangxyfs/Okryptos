// Package filterx 检索候选的 LLM 相关性后置过滤：只删不加，全程 fail-open。
// 依据 docs/2026-09-12-retrieval-noise-solutions.md（后置过滤摆位：
// 输入小、只过滤不改召回、超时即退化为现状）。
package filterx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"okryptos/internal/config"
	"okryptos/internal/index"
	"okryptos/internal/llmx"
)

const systemPrompt = `你是知识库检索结果的相关性过滤器。给定用户输入和若干候选知识条目，判断每条候选是否与用户输入真正相关（对回答或执行该任务有实质帮助）。
只输出一个 JSON 数组，包含相关条目的编号（从 1 开始），例如 [1,3]。若都不相关输出 []。
不要输出任何解释、markdown 标记或其他文字。`

const (
	maxPromptRunes  = 1500 // 用户输入截断（hook 载荷预算）
	maxSummaryRunes = 160  // 单条摘要截断
)

// Filter 用激活的 LLM profile 对 hits 做相关性裁决，返回保留子集（顺序不变）。
// note 仅在实际尝试了 LLM 调用时非空（成功/失败各一行，供调用方记 ok.log）；
// 未启用/未配置 LLM 时原样返回且 note 为空（静默——常态不刷日志）。
// fail-open：超时/调用失败/输出截断/解析失败一律返回原 hits。
func Filter(ctx context.Context, cfg config.Config, prompt string, hits []index.Hit) (kept []index.Hit, note string) {
	if !cfg.Retrieve.Filter.Enabled || len(hits) == 0 {
		return hits, ""
	}
	// 识别意图专用 profile 优先（[llm] active_filter 槽），未配置回退普通 active
	p := cfg.LLM.FilterProfile()
	if p == nil {
		p = cfg.LLM.ActiveProfile()
	}
	if p == nil {
		return hits, ""
	}
	timeout := time.Duration(cfg.Retrieve.Filter.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	maxTokens := cfg.Retrieve.Filter.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 64
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// temperature 不传（llmx.applyTemperature 空值收口，服务端默认兼容性最好）
	rep, err := llmx.New(*p, timeout).Chat(ctx, systemPrompt, userPrompt(prompt, hits), maxTokens)
	if err != nil {
		return hits, fmt.Sprintf("LLM 调用失败，保留全部 %d 条: %v", len(hits), err)
	}
	if rep.Truncated {
		return hits, fmt.Sprintf("LLM 输出截断，保留全部 %d 条", len(hits))
	}
	keepIdx, err := parseKeep(rep.Text, len(hits))
	if err != nil {
		return hits, fmt.Sprintf("LLM 输出解析失败（%v），保留全部 %d 条", err, len(hits))
	}
	inKeep := make(map[int]bool, len(keepIdx))
	for _, i := range keepIdx {
		inKeep[i] = true
	}
	var out []index.Hit
	var dropped []string
	for i, h := range hits {
		if inKeep[i+1] {
			out = append(out, h)
		} else {
			dropped = append(dropped, h.Filename)
		}
	}
	if len(dropped) == 0 {
		return out, fmt.Sprintf("LLM 裁决：%d 条全部保留", len(hits))
	}
	return out, fmt.Sprintf("LLM 裁决：%d→%d 条，丢弃（%s）", len(hits), len(out), strings.Join(dropped, "、"))
}

// userPrompt 组装用户侧载荷：用户输入（截断）+ 编号候选（标题+摘要，消毒截断）。
func userPrompt(prompt string, hits []index.Hit) string {
	var b strings.Builder
	b.WriteString("用户输入：\n")
	b.WriteString(truncateRunes(strings.TrimSpace(prompt), maxPromptRunes))
	b.WriteString("\n\n候选条目：\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] 标题：%s\n摘要：%s\n", i+1,
			index.SanitizeInline(h.Title),
			truncateRunes(index.SanitizeInline(h.Summary), maxSummaryRunes))
	}
	return b.String()
}

// parseKeep 解析模型输出为 1-based 编号列表：容忍 ```json 围栏与前后杂音
// （取首个 [ 到末个 ] 之间的内容），越界/重复编号丢弃；空数组合法（全不相关）。
// fail-closed 收口：原始数组非空但有效编号为 0（编号全部越界/重复）按解析失败
// 返回 error——模型多半没按约定作答，Filter 走"解析失败保留全部"路径。
func parseKeep(text string, n int) ([]int, error) {
	s := strings.TrimSpace(text)
	i, j := strings.Index(s, "["), strings.LastIndex(s, "]")
	if i < 0 || j <= i {
		return nil, fmt.Errorf("未找到 JSON 数组: %q", truncateRunes(s, 80))
	}
	var idx []int
	if err := json.Unmarshal([]byte(s[i:j+1]), &idx); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %v", err)
	}
	seen := make(map[int]bool, len(idx))
	var out []int
	for _, k := range idx {
		if k < 1 || k > n || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	if len(idx) > 0 && len(out) == 0 {
		return nil, fmt.Errorf("编号全部越界/重复: %q", truncateRunes(s[i:j+1], 80))
	}
	return out, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
