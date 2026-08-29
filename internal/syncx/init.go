// init.go：ok sync init 的三情形编排（设计文档 §14），cli 与 gui 共用。
package syncx

import (
	"errors"
	"strings"
)

// InitKind 区分 init 走了哪条路径。
type InitKind int

const (
	InitLocalOnly InitKind = iota // 仅本地历史（remote 为空）
	InitCloned                    // 从远端克隆（本地无知识内容）
	InitPushed                    // 首台设备：本地初始化并推送首个提交
)

// ErrRemoteNotEmpty 表示两边各自初始化过且均有内容——不自动合并（设计文档 §14）。
var ErrRemoteNotEmpty = errors.New("远端仓已有内容，需手动合并一次")

// InitForSync 执行三情形编排。hasContent 由调用方判定（knowledge/ 下有无 .md）。
// remote/config 只在两条成功路径落：克隆成功，或本地 init+commit+SetRemote 后 Push 成功。
// ErrRemoteNotEmpty 属"部分完成"：仓已 init+commit+关联 remote（Push 撞 non-fast-forward），
// 用户手动合并后 ok sync 即可续上。
func (r *Repo) InitForSync(remote string, hasContent bool, commitMsg string) (InitKind, error) {
	if remote == "" {
		if err := r.Init(); err != nil {
			return InitLocalOnly, err
		}
		if _, err := r.CommitAll(commitMsg); err != nil {
			return InitLocalOnly, err
		}
		return InitLocalOnly, nil
	}
	if !hasContent {
		if err := r.CloneToDir(remote); err != nil {
			return InitCloned, err
		}
		return InitCloned, nil
	}
	if err := r.Init(); err != nil {
		return InitPushed, err
	}
	if _, err := r.CommitAll(commitMsg); err != nil {
		return InitPushed, err
	}
	if err := r.SetRemote(remote); err != nil {
		return InitPushed, err
	}
	if err := r.Push(); err != nil {
		var ee *ExitError
		if errors.As(err, &ee) && (strings.Contains(ee.Output, "non-fast-forward") || strings.Contains(ee.Output, "fetch first")) {
			return InitPushed, ErrRemoteNotEmpty
		}
		return InitPushed, err
	}
	return InitPushed, nil
}
