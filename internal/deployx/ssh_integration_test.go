//go:build integration

package deployx

// 真 SSH 冒烟：需要本机 Docker。运行：go test -tags=integration ./internal/deployx/ -run TestSSHIntegration -v
// 起容器：docker run -d --name okdeploy-it -p 2222:2222 \
//   -e PUID=1000 -e PGID=1000 -e USER_NAME=test -e PASSWORD_ACCESS=true -e USER_PASSWORD=testpass \
//   lscr.io/linuxserver/openssh-server:latest
// 跑完清理：docker rm -f okdeploy-it

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSSHIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := DialSSH(ctx, "127.0.0.1", 2222, "test", "testpass", "")
	if err != nil {
		t.Skipf("无集成环境（先起 openssh-server 容器）：%v", err)
	}
	defer c.Close()
	var lines []string
	code, err := c.Run(ctx, "echo hello && echo world", nil, func(s, l string) { lines = append(lines, l) })
	if err != nil || code != 0 || strings.Join(lines, ",") != "hello,world" {
		t.Fatalf("code=%d lines=%v err=%v", code, lines, err)
	}
}
