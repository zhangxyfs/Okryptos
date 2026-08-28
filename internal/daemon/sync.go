package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"openknowledge/internal/config"
	"openknowledge/internal/registry"
	"openknowledge/internal/store"
	"openknowledge/internal/syncx"
)

var syncCheckInterval = time.Minute // 检查周期（包级 var 供测试调小）

// startSyncJanitor 挂同步 ticker（照 run.go:96 自省 ticker 模式，随进程生命周期结束）。
// 每分钟检查一轮：启用同步且到点的项目跑 SyncOnce。失败仅记日志，绝不影响本地链路。
func startSyncJanitor(stdout io.Writer) {
	go func() {
		ticker := time.NewTicker(syncCheckInterval)
		defer ticker.Stop()
		for range ticker.C {
			runSyncCycle(stdout, false)
		}
	}()
}

// runSyncCycle 遍历注册项目，对启用同步且到点（或 force）的项目执行一次同步。
// force=true 跳过 interval 判断（写入防抖触发用，设计文档 §8）。
func runSyncCycle(out io.Writer, force bool) {
	reg, err := registry.Load(registry.DefaultPath())
	if err != nil {
		return
	}
	globalCfg := filepath.Join(registry.Home(), "config.toml")
	for _, p := range reg.Projects {
		st := store.New(filepath.Join(registry.Home(), "projects", p.Name))
		cfg, err := config.LoadMerged(st.ConfigPath(), globalCfg)
		if err != nil || !cfg.Sync.Enabled {
			continue
		}
		if !force && !syncDue(st, cfg.Sync.AutoIntervalMin) {
			continue
		}
		host, _ := os.Hostname()
		msg := fmt.Sprintf("sync: %s %s", host, time.Now().Format(time.RFC3339))
		o := syncx.SyncOnce(st.Root, msg)
		syncx.RecordOutcome(st.Root, st.StateDir(), o)
		switch {
		case o.Err != nil:
			fmt.Fprintf(out, "sync %s: %v\n", p.Name, o.Err)
		case len(o.Conflicts) > 0:
			fmt.Fprintf(out, "sync %s: %d 个文件冲突，待人工解决\n", p.Name, len(o.Conflicts))
		}
	}
}

// syncDue 判断项目是否到同步点（interval<=0 视为关闭自动同步）。
func syncDue(st *store.Store, intervalMin int) bool {
	if intervalMin <= 0 {
		return false
	}
	sf, err := syncx.LoadStatus(st.StateDir())
	if err != nil {
		return true
	}
	last := sf.Layer("personal").LastSync
	if last.IsZero() {
		return true
	}
	return time.Since(last) >= time.Duration(intervalMin)*time.Minute
}
