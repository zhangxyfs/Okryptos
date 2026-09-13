package daemon

import "testing"

// 保活判定：开关开 + exe 在位 + 未运行 → 拉起；其余组合一律不动
//（开关关不拉起已在运行的；运行中不重复拉起——单实例守卫之外的第二道闸）。
func TestDecideLaunchOKMeter(t *testing.T) {
	cases := []struct {
		enabled, exeExists, running bool
		want                        bool
	}{
		{true, true, false, true},
		{true, true, true, false},   // 已运行：不重复拉起
		{false, true, false, false}, // 开关关：不拉起（也不杀）
		{false, true, true, false},
		{true, false, false, false}, // exe 缺失（非 Windows 常态）：空转
		{false, false, false, false},
	}
	for _, c := range cases {
		if got := decideLaunchOKMeter(c.enabled, c.exeExists, c.running); got != c.want {
			t.Fatalf("decideLaunchOKMeter(%v, %v, %v) = %v, want %v",
				c.enabled, c.exeExists, c.running, got, c.want)
		}
	}
}
