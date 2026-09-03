package retrieve

import (
	"strings"
	"testing"
)

func TestCleanQueryStripsKnownBlocks(t *testing.T) {
	prompt := "真正的查询 RetrievalQuirk\n" +
		"[Okryptos] wiki 已落后 3 个 commit，建议更新。\n" +
		"## 必守规约（全文见文件，必要时读取）\n\n- **架构规约** (rule)（/kb/规约.md）\n\n" +
		"## 相关知识（需要全文时读取对应文件）\n\n- **干扰条目** (note) — DistractionQuirk（/kb/干扰.md）\n"
	got := CleanQuery(prompt)
	if strings.Contains(got, "DistractionQuirk") || strings.Contains(got, "干扰条目") {
		t.Errorf("检索命中块应整体剥离，got: %q", got)
	}
	if strings.Contains(got, "必守规约") || strings.Contains(got, "架构规约") {
		t.Errorf("粘性指针块应剥离，got: %q", got)
	}
	if strings.Contains(got, "[Okryptos]") {
		t.Errorf("提示行应剥离，got: %q", got)
	}
	if !strings.Contains(got, "RetrievalQuirk") {
		t.Errorf("真实查询应保留，got: %q", got)
	}
}

func TestCleanQueryStickyBlockKeepsFollowingSection(t *testing.T) {
	// 粘性指针块在中间、后面还有别的 "## " 小节：只剥指针块，后续小节保留
	prompt := "问题\n## 必守规约（全文见文件，必要时读取）\n\n- **甲** (rule)（/kb/甲.md）\n\n## 用户自己写的章节\n\n保留我 KeepMe\n"
	got := CleanQuery(prompt)
	if strings.Contains(got, "必守规约") || strings.Contains(got, "（/kb/甲.md）") {
		t.Errorf("粘性指针块应剥离，got: %q", got)
	}
	if !strings.Contains(got, "KeepMe") {
		t.Errorf("后续用户章节应保留，got: %q", got)
	}
}

func TestCleanQueryPlainPromptUntouched(t *testing.T) {
	p := "普通 prompt 没有注入块"
	if got := CleanQuery(p); got != p {
		t.Errorf("普通 prompt 不应改变，got: %q", got)
	}
}
