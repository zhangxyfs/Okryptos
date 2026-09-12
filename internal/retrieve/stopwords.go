package retrieve

import "math"

// stopwords.go 中文虚词/低判别力词元表：只作用于关键词通道的"命中词元覆盖度"
// 准入计数（index.queryAll），不进入索引文本、不影响 FTS 打分与排序。
// 依据 docs/2026-09-12-retrieval-noise-solutions.md 方案A：虚词 bigram
// （"还是""底的"类）是短口语查询误注入的主要来源。

// builtinStopTerms 内置虚词词元表（编译进二进制、随版本演进，与门控短语表同惯例；
// 用户追加层走 [retrieve.coverage] extra_stop_terms）。Terms 只产出单字与二元组，
// 表内条目不超 2 字。
var builtinStopTerms = map[string]bool{
	// 单字虚词（孤立单字 CJK 产出单字词元）
	"的": true, "了": true, "吗": true, "呢": true, "吧": true, "啊": true,
	"和": true, "与": true, "或": true, "在": true, "是": true, "有": true,
	"我": true, "你": true, "他": true, "她": true, "它": true, "这": true,
	"那": true, "就": true, "都": true, "也": true, "还": true, "不": true,
	"没": true, "很": true, "太": true, "被": true, "把": true, "给": true,
	// 高频虚词/跨词界假词二元组
	"还是": true, "就是": true, "底的": true, "了的": true, "的是": true,
	"什么": true, "怎么": true, "这个": true, "那个": true, "我们": true,
	"你们": true, "他们": true, "可以": true, "没有": true, "一下": true,
	"现在": true, "已经": true, "这样": true, "那样": true, "知道": true,
	"觉得": true, "应该": true, "可能": true, "但是": true, "因为": true,
	"所以": true, "如果": true, "然后": true, "一个": true, "一些": true,
	"有点": true, "非常": true, "特别": true, "真的": true,
}

// EffectiveTerms 过滤停用词元后的有效词元（保序去重）：覆盖度计数只认有效词元，
// 防止虚词"助攻"无关条目过准入。extra 为配置追加层，与内置表取并集。
func EffectiveTerms(terms []string, extra []string) []string {
	var extraSet map[string]bool
	if len(extra) > 0 {
		extraSet = make(map[string]bool, len(extra))
		for _, t := range extra {
			extraSet[t] = true
		}
	}
	seen := make(map[string]bool, len(terms))
	var out []string
	for _, t := range terms {
		if builtinStopTerms[t] || extraSet[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// RequiredCoverage 关键词准入要求的最少命中有效词元数（minimum_should_match
// 语义）：n=0 → 0（调用方据此跳过关键词通道）；n≥1 → min(n, max(2, ⌈n×ratio⌉))，
// 即单词元查询要求命中它自己，多词元查询至少命中 2 个且不低于 ratio 比例。
// ratio<=0 或 >1 按 0.5（fail-open 方向取默认）。
func RequiredCoverage(n int, ratio float64) int {
	if n <= 0 {
		return 0
	}
	if ratio <= 0 || ratio > 1 {
		ratio = 0.5
	}
	need := int(math.Ceil(float64(n) * ratio))
	if need < 2 {
		need = 2
	}
	if need > n {
		need = n
	}
	return need
}
