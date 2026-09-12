package retrieve

import (
	"reflect"
	"testing"
)

func TestEffectiveTerms(t *testing.T) {
	cases := []struct {
		name  string
		terms []string
		extra []string
		want  []string
	}{
		{"事故词元：虚词被滤", []string{"还是", "是黑", "黑底", "底的"}, nil, []string{"是黑", "黑底"}},
		{"纯虚词", []string{"还是", "就是"}, nil, nil},
		{"实词保留且去重", []string{"构建", "双路", "构建"}, nil, []string{"构建", "双路"}},
		{"追加层生效", []string{"甲乙", "丙丁"}, []string{"甲乙"}, []string{"丙丁"}},
		{"空输入", nil, nil, nil},
	}
	for _, c := range cases {
		if got := EffectiveTerms(c.terms, c.extra); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: EffectiveTerms(%v, %v) = %v, want %v", c.name, c.terms, c.extra, got, c.want)
		}
	}
}

func TestRequiredCoverage(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 0}, {1, 1}, {2, 1}, {3, 1}, {4, 1}, {5, 2}, {6, 2}, {8, 2},
	}
	for _, c := range cases {
		if got := RequiredCoverage(c.n, 0.25); got != c.want {
			t.Errorf("RequiredCoverage(%d, 0.25) = %d, want %d", c.n, got, c.want)
		}
	}
	if got := RequiredCoverage(4, 0); got != 1 {
		t.Errorf("非法 ratio 应按 0.25: got %d", got)
	}
	if got := RequiredCoverage(8, 0.75); got != 6 {
		t.Errorf("RequiredCoverage(8, 0.75) = %d, want 6", got)
	}
}
