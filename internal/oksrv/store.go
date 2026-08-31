// Package oksrv 是 okserver 的服务端管理面：SQLite 存储 + 认证 + GitBackend + HTTP API。
// 仅依赖标准库 + modernc.org/sqlite + x/crypto(bcrypt)。
package oksrv

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store 是 okserver 的 SQLite 存储句柄。
type Store struct {
	db  *sql.DB
	dir string
}

// schema 建表语句（六表 + meta KV）。时间列一律 TEXT 存 UTC RFC3339，
// 避免时区歧义；sessions 表由认证层使用，本文件仅建表。
const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  disabled INTEGER NOT NULL DEFAULT 0,
  must_change_password INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT NOT NULL PRIMARY KEY,
  user_id INTEGER NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS orgs (
  name TEXT NOT NULL PRIMARY KEY,
  description TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS org_members (
  org TEXT NOT NULL,
  username TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'member',
  PRIMARY KEY (org, username)
);
CREATE TABLE IF NOT EXISTS repos (
  layer TEXT NOT NULL,
  owner TEXT NOT NULL,
  project TEXT NOT NULL,
  clone_url TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (layer, owner, project)
);
CREATE TABLE IF NOT EXISTS audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
  key TEXT NOT NULL PRIMARY KEY,
  value TEXT NOT NULL
);
`

// OpenStore 打开（或创建）<dataDir>/okserver.db。
func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	// busy_timeout/WAL/synchronous=NORMAL 的取舍照 internal/index/db.go 先例：
	// 管理面并发行低，但同一库可能被 okserver 与调试工具同时打开
	dsn := "file:" + filepath.ToSlash(filepath.Join(dataDir, "okserver.db")) +
		"?_pragma=busy_timeout(3000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建 schema 失败: %w", err)
	}
	// 存量库补列：CREATE TABLE IF NOT EXISTS 不会改旧表，PRAGMA 检测 + ALTER，幂等
	if err := migrateUserColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移 users 表失败: %w", err)
	}
	return &Store{db: db, dir: dataDir}, nil
}

// migrateUserColumns 给存量库补 users 表后加列。
func migrateUserColumns(db *sql.DB) error {
	has, err := hasColumn(db, "users", "must_change_password")
	if err != nil {
		return err
	}
	if !has {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

// hasColumn 用 PRAGMA table_info 检测列是否存在。
func hasColumn(db *sql.DB, table, col string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == col {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close 关闭存储句柄。
func (s *Store) Close() error { return s.db.Close() }

// nowUTC 返回当前 UTC 时间的 RFC3339 串（全库时间统一格式）。
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// parseTime 把库里的 RFC3339 TEXT 还原为 time.Time；解析失败给零值，
// 不阻塞读取（写入侧恒为 nowUTC 产物，正常不会失败）。
func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339, v)
	return t
}

// User 是一个管理面账号；Role ∈ root/admin/member，root 全库唯一。
// MustChangePassword=true 表示持初始/重置密码，HTTP 层拦截其改密外的一切请求。
type User struct {
	ID                 int64
	Username           string
	Role               string
	Disabled           bool
	MustChangePassword bool
	CreatedAt          time.Time
}

// CreateUser 建账号。username 重复（UNIQUE）或已有 root 再建 root 时返回错误：
// root 唯一性靠应用层检查（SQLite 无法表达"role='root' 至多一行"的部分唯一约束）。
func (s *Store) CreateUser(username, role, bcryptHash string) (*User, error) {
	if role == "root" && s.HasRoot() {
		return nil, fmt.Errorf("root 已存在，拒绝再建 root")
	}
	res, err := s.db.Exec(
		`INSERT INTO users(username,role,password_hash,created_at) VALUES(?,?,?,?)`,
		username, role, bcryptHash, nowUTC())
	if err != nil {
		return nil, fmt.Errorf("创建用户 %s 失败: %w", username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.getUserByID(id), nil
}

// scanUser 从一行 users 查询结果扫出 User；created_at TEXT 还原为 time.Time。
func scanUser(scan func(dest ...any) error) (*User, error) {
	var u User
	var disabled, mustChange int
	var createdAt string
	if err := scan(&u.ID, &u.Username, &u.Role, &disabled, &mustChange, &createdAt); err != nil {
		return nil, err
	}
	u.Disabled = disabled != 0
	u.MustChangePassword = mustChange != 0
	u.CreatedAt = parseTime(createdAt)
	return &u, nil
}

// getUserByID 按主键读用户；CreateUser 建完即读，保证返回的 CreatedAt 与库一致。
func (s *Store) getUserByID(id int64) *User {
	u, err := scanUser(s.db.QueryRow(
		`SELECT id,username,role,disabled,must_change_password,created_at FROM users WHERE id=?`, id).Scan)
	if err != nil {
		return nil
	}
	return u
}

// GetUser 按用户名读用户；不存在返回 nil（调用方用 nil 判定"无此账号"）。
func (s *Store) GetUser(username string) *User {
	u, err := scanUser(s.db.QueryRow(
		`SELECT id,username,role,disabled,must_change_password,created_at FROM users WHERE username=?`, username).Scan)
	if err != nil {
		return nil
	}
	return u
}

// ListUsers 按创建顺序列出全部用户；查询失败返回空切片（列表为空与出错同态，
// 管理面列表场景下不出错页，错误会由后续写操作暴露）。
func (s *Store) ListUsers() []User {
	rows, err := s.db.Query(`SELECT id,username,role,disabled,must_change_password,created_at FROM users ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, *u)
	}
	return out
}

// SetUserDisabled 禁用/启用账号。root 拒绝禁用——禁用唯一 root 会把管理面锁死，
// 此处是最后一道防线（HTTP 层另有自己的校验）。
func (s *Store) SetUserDisabled(username string, disabled bool) error {
	if disabled {
		if u := s.GetUser(username); u != nil && u.Role == "root" {
			return fmt.Errorf("root 不可禁用")
		}
	}
	d := 0
	if disabled {
		d = 1
	}
	_, err := s.db.Exec(`UPDATE users SET disabled=? WHERE username=?`, d, username)
	return err
}

// SetUserPasswordHash 换密码哈希（bcrypt 哈希由调用方算好，存储层不碰明文）。
func (s *Store) SetUserPasswordHash(username, bcryptHash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE username=?`, bcryptHash, username)
	return err
}

// SetMustChangePassword 置/清"必须改密"标记（初始/重置密码置 1，自助改密成功清 0）。
func (s *Store) SetMustChangePassword(username string, v bool) error {
	n := 0
	if v {
		n = 1
	}
	_, err := s.db.Exec(`UPDATE users SET must_change_password=? WHERE username=?`, n, username)
	return err
}

// DeleteUserSessionsExcept 删除该用户除 keepTokenHash 外的全部会话；
// keepTokenHash 传空串即删全部（管理员重置/reset-root 场景）。
func (s *Store) DeleteUserSessionsExcept(userID int64, keepTokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=? AND token_hash != ?`, userID, keepTokenHash)
	return err
}

// DeleteUser 删账号：连带清 sessions 与 org_members；repos/audit 行保留（历史登记）。
// root 拒删——与 SetUserDisabled 同理，删唯一 root 会把管理面锁死，此处是最后防线。
func (s *Store) DeleteUser(username string) error {
	u := s.GetUser(username)
	if u == nil {
		return fmt.Errorf("用户 %q 不存在", username)
	}
	if u.Role == "root" {
		return fmt.Errorf("root 不可删除")
	}
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, u.ID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM org_members WHERE username=?`, username); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM users WHERE username=?`, username)
	return err
}

// HasRoot 报告是否已初始化 root（首次启动引导流程的判定依据）。
func (s *Store) HasRoot() bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='root'`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// ResetRoot 重置 root 密码（root 初始密码只显示一次，丢失后的恢复入口）：
// 重生成 32 位随机密码，bcrypt 更新 root 行，重写 <dataDir>/INITIAL_ROOT_PASSWORD（0600），
// 返回明文（只此一次）。root 行由首启 EnsureRoot 建立，这里只做 UPDATE；未初始化时报错。
func (s *Store) ResetRoot() (string, error) {
	if !s.HasRoot() {
		return "", fmt.Errorf("root 尚未初始化，无法重置")
	}
	pw := GenerateSecret(24) // 24 字节 → 32 字符
	hash, err := HashPassword(pw)
	if err != nil {
		return "", err
	}
	if err := s.SetUserPasswordHash("root", hash); err != nil {
		return "", fmt.Errorf("更新 root 密码失败: %w", err)
	}
	// 重置 = 强制改密 + 踢掉 root 全部旧会话
	if err := s.SetMustChangePassword("root", true); err != nil {
		return "", err
	}
	if root := s.GetUser("root"); root != nil {
		if err := s.DeleteUserSessionsExcept(root.ID, ""); err != nil {
			return "", err
		}
	}
	initFile := filepath.Join(s.dir, "INITIAL_ROOT_PASSWORD")
	if err := os.WriteFile(initFile, []byte(pw+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("重写 INITIAL_ROOT_PASSWORD 失败: %w", err)
	}
	return pw, nil
}

// Org 是一个组织（对应 Gitea org，仓库归属的组织层命名空间）。
type Org struct {
	Name      string
	Desc      string
	CreatedAt time.Time
}

// CreateOrg 建组织；重名由主键约束报错（创建属低频管理操作，不做幂等）。
func (s *Store) CreateOrg(name, desc string) error {
	_, err := s.db.Exec(`INSERT INTO orgs(name,description,created_at) VALUES(?,?,?)`,
		name, desc, nowUTC())
	return err
}

// ListOrgs 按名列出全部组织。
func (s *Store) ListOrgs() []Org {
	rows, err := s.db.Query(`SELECT name,description,created_at FROM orgs ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Org
	for rows.Next() {
		var o Org
		var createdAt string
		if err := rows.Scan(&o.Name, &o.Desc, &createdAt); err != nil {
			return out
		}
		o.CreatedAt = parseTime(createdAt)
		out = append(out, o)
	}
	return out
}

// OrgExists 报告组织是否存在。
func (s *Store) OrgExists(name string) bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM orgs WHERE name=?`, name).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// OrgMember 是组织成员关系；Role 为组织内角色（如 owner/member）。
type OrgMember struct {
	Org      string
	Username string
	Role     string
}

// AddOrgMember 加成员；INSERT OR REPLACE 使重复添加幂等（兼作改角色）。
func (s *Store) AddOrgMember(org, username, role string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO org_members(org,username,role) VALUES(?,?,?)`,
		org, username, role)
	return err
}

// RemoveOrgMember 移除成员；本就不存在也算成功（DELETE 天然幂等）。
func (s *Store) RemoveOrgMember(org, username string) error {
	_, err := s.db.Exec(`DELETE FROM org_members WHERE org=? AND username=?`, org, username)
	return err
}

// ListOrgMembers 按用户名列出组织全部成员。
func (s *Store) ListOrgMembers(org string) []OrgMember {
	rows, err := s.db.Query(
		`SELECT org,username,role FROM org_members WHERE org=? ORDER BY username`, org)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []OrgMember
	for rows.Next() {
		var m OrgMember
		if err := rows.Scan(&m.Org, &m.Username, &m.Role); err != nil {
			return out
		}
		out = append(out, m)
	}
	return out
}

// ListUserOrgs 列出用户所在的全部组织名（权限判定用）。
func (s *Store) ListUserOrgs(username string) []string {
	rows, err := s.db.Query(`SELECT org FROM org_members WHERE username=? ORDER BY org`, username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return out
		}
		out = append(out, name)
	}
	return out
}

// Repo 是一个已登记仓库；(Layer, Owner, Project) 三元组唯一标识。
type Repo struct {
	Layer     string
	Owner     string
	Project   string
	CloneURL  string
	CreatedBy string
	CreatedAt time.Time
}

// UpsertRepo 登记仓库；同三元组重复登记只更新 clone_url（重建仓库场景 URL 可能变，
// 创建者与创建时间保留首次记录）。
func (s *Store) UpsertRepo(r Repo) error {
	_, err := s.db.Exec(
		`INSERT INTO repos(layer,owner,project,clone_url,created_by,created_at) VALUES(?,?,?,?,?,?)
		 ON CONFLICT(layer,owner,project) DO UPDATE SET clone_url=excluded.clone_url`,
		r.Layer, r.Owner, r.Project, r.CloneURL, r.CreatedBy, nowUTC())
	return err
}

// scanRepo 从一行 repos 查询结果扫出 Repo。
func scanRepo(scan func(dest ...any) error) (*Repo, error) {
	var r Repo
	var createdAt string
	if err := scan(&r.Layer, &r.Owner, &r.Project, &r.CloneURL, &r.CreatedBy, &createdAt); err != nil {
		return nil, err
	}
	r.CreatedAt = parseTime(createdAt)
	return &r, nil
}

// GetRepo 按三元组读仓库；不存在返回 nil。
func (s *Store) GetRepo(layer, owner, project string) *Repo {
	r, err := scanRepo(s.db.QueryRow(
		`SELECT layer,owner,project,clone_url,created_by,created_at FROM repos
		 WHERE layer=? AND owner=? AND project=?`, layer, owner, project).Scan)
	if err != nil {
		return nil
	}
	return r
}

// queryRepos 是 ListRepos/ListReposByOwner 共用的多行扫描。
func (s *Store) queryRepos(query string, args ...any) []Repo {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Repo
	for rows.Next() {
		r, err := scanRepo(rows.Scan)
		if err != nil {
			return out
		}
		out = append(out, *r)
	}
	return out
}

// ListRepos 按三元组序列出全部仓库。
func (s *Store) ListRepos() []Repo {
	return s.queryRepos(
		`SELECT layer,owner,project,clone_url,created_by,created_at FROM repos
		 ORDER BY layer,owner,project`)
}

// ListReposByOwner 列出某层某 owner 下的全部仓库（个人/组织仓库列表页用）。
func (s *Store) ListReposByOwner(layer, owner string) []Repo {
	return s.queryRepos(
		`SELECT layer,owner,project,clone_url,created_by,created_at FROM repos
		 WHERE layer=? AND owner=? ORDER BY project`, layer, owner)
}

// AuditEntry 是一条审计记录（谁在何时对什么做了什么）。
type AuditEntry struct {
	ID        int64
	Actor     string
	Action    string
	Target    string
	Detail    string
	CreatedAt time.Time
}

// Audit 追加一条审计记录。尽力而为：写失败只记 stderr 不中断业务操作，
// 否则审计库异常会连带正常管理操作一起失败。
func (s *Store) Audit(actor, action, target, detail string) {
	if _, err := s.db.Exec(
		`INSERT INTO audit(actor,action,target,detail,created_at) VALUES(?,?,?,?,?)`,
		actor, action, target, detail, nowUTC()); err != nil {
		fmt.Fprintf(os.Stderr, "okserver: 审计写入失败（%s %s %s）: %v\n", actor, action, target, err)
	}
}

// ListAudit 按时间倒序（新的在前）分页读审计记录。
func (s *Store) ListAudit(limit, offset int) []AuditEntry {
	rows, err := s.db.Query(
		`SELECT id,actor,action,target,detail,created_at FROM audit ORDER BY id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Actor, &e.Action, &e.Target, &e.Detail, &createdAt); err != nil {
			return out
		}
		e.CreatedAt = parseTime(createdAt)
		out = append(out, e)
	}
	return out
}

// GetMeta 读元数据（schema 版本等）；不存在返回 ""。
func (s *Store) GetMeta(key string) string {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err != nil { // 含 sql.ErrNoRows（键不存在），与真错误同态返回 ""
		return ""
	}
	return v
}

// SetMeta 写元数据；同 key 覆盖（upsert）。
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}
