package deployx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHClient 用 golang.org/x/crypto/ssh 实现 Executor（零新增依赖：go.mod 已有 x/crypto）。
type SSHClient struct {
	cli *ssh.Client
}

// 编译期接口断言：SSHClient 必须实现 Executor。
var _ Executor = (*SSHClient)(nil)

// DialSSH 连接 SSH；keyPath 非空用私钥，password 非空加密码兜底；两者皆空报错。
// v1 接受任意主机密钥（InsecureIgnoreHostKey）：部署目标多为内网 NAS，
// 首次连接无 known_hosts 可校验；风险提示见 server/deploy/README.md。
func DialSSH(ctx context.Context, host string, port int, user, password, keyPath string) (*SSHClient, error) {
	var auth []ssh.AuthMethod
	if keyPath != "" {
		pem, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("读私钥：%w", err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			return nil, fmt.Errorf("解析私钥：%w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if password != "" {
		auth = append(auth, ssh.Password(password))
	}
	if len(auth) == 0 {
		return nil, errors.New("需要密码或私钥")
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // 见函数注释
		Timeout:         10 * time.Second,
	}
	type res struct {
		c   *ssh.Client
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), cfg)
		ch <- res{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("SSH 连接 %s:%d 失败：%w", host, port, r.err)
		}
		return &SSHClient{cli: r.c}, nil
	}
}

// Run 实现 Executor.Run：stdout/stderr 行级流式回调，退出码返回。
func (c *SSHClient) Run(ctx context.Context, cmd string, stdin io.Reader, onLine func(string, string)) (int, error) {
	sess, err := c.cli.NewSession()
	if err != nil {
		return -1, err
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return -1, err
	}
	if stdin != nil {
		sess.Stdin = stdin
	}
	if err := sess.Start(cmd); err != nil {
		return -1, err
	}
	var wg sync.WaitGroup
	scan := func(r io.Reader, name string) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			if onLine != nil {
				onLine(name, sc.Text())
			}
		}
	}
	wg.Add(2)
	go scan(stdout, "stdout")
	go scan(stderr, "stderr")
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		// SIGKILL 可能被服务端拒绝（远端进程不死、Wait 不返回），
		// 故 Signal 后立即 Close channel，让 Wait 必返回错误，避免挂死。
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		<-done
		wg.Wait()
		return -1, ctx.Err()
	case werr := <-done:
		wg.Wait()
		if werr == nil {
			return 0, nil
		}
		var ee *ssh.ExitError
		if errors.As(werr, &ee) {
			return ee.ExitStatus(), nil
		}
		return -1, werr
	}
}

// Download 实现 Executor.Download：远端 cmd 的 stdout 流写入 w。
func (c *SSHClient) Download(ctx context.Context, cmd string, w io.Writer, onProgress func(int64)) error {
	sess, err := c.cli.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf syncBuffer
	sess.Stderr = &errBuf
	if err := sess.Start(cmd); err != nil {
		return err
	}
	pw := &countWriter{w: w, cb: onProgress}
	copyCh := make(chan error, 1)
	go func() { _, err := io.Copy(pw, stdout); copyCh <- err }()
	waitCh := make(chan error, 1)
	go func() { waitCh <- sess.Wait() }()
	select {
	case <-ctx.Done():
		// SIGKILL 可能被服务端拒绝，Signal 后立即 Close 兜底，防止 Wait 挂死。
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		<-waitCh
		return ctx.Err()
	case cerr := <-copyCh:
		if cerr != nil {
			// copy 失败同样先 SIGKILL 再 Close 兜底，确保 Wait 必返回。
			_ = sess.Signal(ssh.SIGKILL)
			_ = sess.Close()
			<-waitCh
			return cerr
		}
		if werr := <-waitCh; werr != nil {
			return fmt.Errorf("远端命令失败：%w：%s", werr, errBuf.String())
		}
		return nil
	}
}

func (c *SSHClient) Close() error { return c.cli.Close() }

// countWriter 统计写入字节并回调进度。
type countWriter struct {
	w  io.Writer
	n  int64
	cb func(int64)
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.cb != nil {
		c.cb(c.n)
	}
	return n, err
}

// syncBuffer 是 goroutine 安全的字节缓冲（收集 stderr）。
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) < 4096 { // 只留尾部，防膨胀
		b.buf = append(b.buf, p...)
		if len(b.buf) > 4096 {
			b.buf = b.buf[len(b.buf)-4096:]
		}
	}
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
