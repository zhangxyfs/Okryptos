// auth.go：bcrypt 密码、root 首启、会话 token、登录限流。
package oksrv

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = 30 * 24 * time.Hour // 30 天（spec §17 待决建议值）

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// GenerateSecret 生成 n 字节随机的 base64url（无填充）密钥。
func GenerateSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand 失败是不可恢复的系统故障
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// EnsureRoot 首启初始化：无 root 时生成 root + 32 位随机密码（明文只返回这一次）。
func (s *Store) EnsureRoot() (string, bool, error) {
	if s.HasRoot() {
		return "", false, nil
	}
	pw := GenerateSecret(24) // 24 字节 → 32 字符
	hash, err := HashPassword(pw)
	if err != nil {
		return "", false, err
	}
	if _, err := s.CreateUser("root", "root", hash); err != nil {
		return "", false, err
	}
	// 初始 root 密码强制首登改密（自助改密成功后清除）
	if err := s.SetMustChangePassword("root", true); err != nil {
		return "", false, err
	}
	return pw, true, nil
}

// VerifyLogin 校验用户名密码；disabled 拒绝；失败一律 nil（不区分原因，防枚举）。
func (s *Store) VerifyLogin(username, password string) *User {
	u := s.GetUser(username)
	if u == nil || u.Disabled {
		return nil
	}
	var hash string
	if err := s.db.QueryRow("SELECT password_hash FROM users WHERE id=?", u.ID).Scan(&hash); err != nil {
		return nil
	}
	if !CheckPassword(hash, password) {
		return nil
	}
	return u
}

// CreateSession 颁会话 token（明文只下发这一次，库存 SHA-256）。
func (s *Store) CreateSession(userID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	_, err := s.db.Exec("INSERT INTO sessions(token_hash, user_id, expires_at, created_at) VALUES(?,?,?,?)",
		hex.EncodeToString(sum[:]), userID, now.Add(sessionTTL).Format(time.RFC3339), now.Format(time.RFC3339))
	return token, err
}

// SessionUser 按 token 查用户；过期/不存在/disabled → nil。
func (s *Store) SessionUser(token string) *User {
	sum := sha256.Sum256([]byte(token))
	var userID int64
	var expires string
	err := s.db.QueryRow("SELECT user_id, expires_at FROM sessions WHERE token_hash=?", hex.EncodeToString(sum[:])).Scan(&userID, &expires)
	if err != nil {
		return nil
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || time.Now().UTC().After(exp) {
		return nil
	}
	// created_at 落库是 TEXT，直接 Scan 进 time.Time 会报类型错误，
	// 走 store.go 的 scanUser（先扫 string 再 parseTime）。
	u, err := scanUser(s.db.QueryRow(
		"SELECT id, username, role, disabled, must_change_password, created_at FROM users WHERE id=?", userID).Scan)
	if err != nil || u.Disabled {
		return nil
	}
	return u
}

// LoginLimiter 登录限流：每用户名失败 5 次锁 5 分钟（内存态，重启清零）。
type LoginLimiter struct {
	mu         sync.Mutex
	fails      map[string]int
	lockedTill map[string]time.Time
	failWindow time.Duration
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{fails: map[string]int{}, lockedTill: map[string]time.Time{}, failWindow: 5 * time.Minute}
}

func (l *LoginLimiter) Allow(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	till, ok := l.lockedTill[username]
	return !ok || time.Now().After(till)
}

func (l *LoginLimiter) Fail(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[username]++
	if l.fails[username] >= 5 {
		l.lockedTill[username] = time.Now().Add(l.failWindow)
		l.fails[username] = 0
	}
}

func (l *LoginLimiter) Reset(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, username)
	delete(l.lockedTill, username)
}

// bearerTokenHash 返回请求携带的 Bearer token 的库存哈希（SHA-256 hex）；
// 无 Bearer 头返回空串。改密后"踢其他会话保当前"用。
func bearerTokenHash(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) <= 7 || h[:7] != "Bearer " {
		return ""
	}
	sum := sha256.Sum256([]byte(h[7:]))
	return hex.EncodeToString(sum[:])
}
