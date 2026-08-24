package fsx

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	lockTimeout = 5 * time.Second // 拿锁最长等待：hook 有 10s 宿主上限，不能等满
	// 锁龄超此值视为持有者崩溃残留，可抢占。临界区含 fsx.WriteFile 的
	// fsync+rename，杀软实时扫描下单次可达数秒——2s 阈值会把仍在执行 fn 的
	// 存活持有者误判为崩溃而抢占，两方并行进入临界区，恰好复现锁要防的
	// 丢注册/丢更新。取 15s：正常与慢临界区都在数秒内完成，锁龄 15s 未释放
	// 基本只剩持有者崩溃一种解释。
	lockStaleAge = 15 * time.Second
)

// WithFileLock 以 path+".lock" 的 O_EXCL 锁文件实现跨进程互斥，锁内容为持有者
// token，释放时只删自己的锁（超时被抢占后不误删新持有者）。临界区应是快操作
// （Load→改→原子写），但慢盘/杀软扫描下可达数秒，因此锁龄未达 lockStaleAge 的
// 他方锁一律不抢占（无法与存活持有者区分）；等锁到 lockTimeout 仍拿不到则
// fail-open 无锁执行——配合原子写，竞态窗口远小于干脆不锁，且绝不能因等锁卡死
// 宿主 hook。lockStaleAge > lockTimeout：持有者崩溃的残留锁先由 fail-open 保证
// 功能可用，锁龄越过 lockStaleAge 后由后续调用方抢占回收。
// 返回 fn 的错误；抢占/fail-open 路径同样执行并返回 fn 的错误。
func WithFileLock(path string, fn func() error) error {
	return withFileLock(path, fn, false)
}

// WithFileLockStrict 与 WithFileLock 相同，但等满 lockTimeout 仍拿不到锁时返回
// 错误而非无锁执行：注册表这类强一致数据的读-改-写不能 fail-open——两个进程
// 同时无锁裸跑正是"丢项目注册"的复现路径。hook 的 state 会话锁请用 WithFileLock。
// 注意 lockStaleAge > lockTimeout：持有者刚崩溃时其残留锁会让严格调用先超时报错，
// 锁龄越过 lockStaleAge 后由后续调用方抢占恢复（抢占会记日志）。
func WithFileLockStrict(path string, fn func() error) error {
	return withFileLock(path, fn, true)
}

func withFileLock(path string, fn func() error, strict bool) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	lp := path + ".lock"
	token := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(token)
			_ = f.Close()
			err := fn()
			// 只删除仍属于本持有者的锁：超时被抢占后文件归新持有者
			if data, rerr := os.ReadFile(lp); rerr == nil && string(data) == token {
				_ = os.Remove(lp)
			}
			return err
		}
		if fi, statErr := os.Stat(lp); statErr == nil && time.Since(fi.ModTime()) > lockStaleAge {
			// 抢占先 rename 再删：rename 只对第一个等待者成功，消除 stat→remove
			// 竞态下后来者把新持有者刚创建的锁一并删掉的窗口
			stale := lp + ".stale-" + token
			if os.Rename(lp, stale) == nil {
				log.Printf("fsx: 抢占陈旧文件锁 %s（锁龄 %v > %v，持有者疑似崩溃；若持有者实为慢临界区存活，请排查磁盘/杀软扫描）", lp, time.Since(fi.ModTime()).Round(time.Second), lockStaleAge)
				_ = os.Remove(stale)
			}
			continue
		}
		if time.Now().After(deadline) {
			if strict {
				return fmt.Errorf("等待文件锁超时（%s 被占用）", lp)
			}
			return fn()
		}
		time.Sleep(15 * time.Millisecond)
	}
}
