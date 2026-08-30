// Package deployx 是 okdeploy 一键部署器的核心：SSH 远端执行、环境探测、
// 部署/升级/备份/恢复/卸载任务编排、SSE 日志。
// 设计文档：docs/superpowers/specs/2026-08-28-okdeploy-design.md
package deployx

import (
	"context"
	"io"
	"sync"
	"time"
)

// Executor 抽象"在远端机器上执行操作"。SSH 实现见 ssh.go；测试用 fake。
type Executor interface {
	// Run 执行 shell 命令；stdin 可为 nil；每输出一行回调 onLine("stdout"|"stderr", line)。
	// onLine 可能被 stdout/stderr 两个 goroutine 并发调用，实现方须保证回调线程安全（LogHub.Publish 已安全）。
	// 返回进程退出码；仅传输层/ctx 错误返回非 nil err（命令非零退出不算 err）。
	Run(ctx context.Context, cmd string, stdin io.Reader, onLine func(stream, line string)) (code int, err error)
	// Download 执行远端 cmd 并把其 stdout 流写入 w；onProgress 回传累计字节数。
	Download(ctx context.Context, cmd string, w io.Writer, onProgress func(n int64)) error
	Close() error
}

// LogEvent 是一条任务日志。
type LogEvent struct {
	Time  time.Time `json:"ts"`
	Step  string    `json:"step"`
	Level string    `json:"level"` // info|ok|err
	Text  string    `json:"text"`
}

// LogHub 收集日志并向订阅者广播（保留全量历史，迟到的订阅者先补历史）。
type LogHub struct {
	mu      sync.Mutex
	history []LogEvent
	subs    map[chan LogEvent]struct{}
}

func NewLogHub() *LogHub { return &LogHub{subs: map[chan LogEvent]struct{}{}} }

func (h *LogHub) Publish(step, level, text string) {
	ev := LogEvent{Time: time.Now(), Step: step, Level: level, Text: text}
	h.mu.Lock()
	h.history = append(h.history, ev)
	for ch := range h.subs {
		select {
		case ch <- ev:
		default: // 慢订阅者丢帧不阻塞任务；重连后可从 History 补齐
		}
	}
	h.mu.Unlock()
}

// Subscribe 返回缓冲 channel 与退订函数。
func (h *LogHub) Subscribe() (<-chan LogEvent, func()) {
	ch := make(chan LogEvent, 1024)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *LogHub) History() []LogEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]LogEvent(nil), h.history...)
}
