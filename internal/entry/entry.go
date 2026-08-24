package entry

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Entry struct {
	Title     string   `yaml:"title"`
	Type      string   `yaml:"type"`
	Tags      []string `yaml:"tags"`
	Mandatory bool     `yaml:"mandatory"`
	Draft     bool     `yaml:"draft"` // 草稿不参与检索注入，INDEX.md 标【草稿】
	// Archived 归档条目：不进 INDEX 主列表，仍保留在库中可检索
	Archived bool `yaml:"archived,omitempty"`
	// Created 创建日期（YYYY-MM-DD），供归档候选报告；历史条目缺省为空
	Created string   `yaml:"created,omitempty"`
	Summary string   `yaml:"summary"`
	Body    string   `yaml:"-"`
	Path    string   `yaml:"-"`
}

var validTypes = map[string]bool{"rule": true, "pitfall": true, "note": true, "reference": true}

func ValidType(t string) bool { return validTypes[t] }

// Parse 解析 "---\n<yaml>\n---\n<body>" 格式的条目文件；容忍 CRLF 与 UTF-8 BOM。
func Parse(content []byte) (*Entry, error) {
	s := strings.TrimPrefix(string(content), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, fmt.Errorf("缺少 frontmatter 起始 ---")
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	delim := "\n---\n"
	if end < 0 && strings.HasSuffix(rest, "\n---") {
		end = len(rest) - len("\n---")
		delim = "\n---"
	}
	if end < 0 {
		return nil, fmt.Errorf("缺少 frontmatter 结束 ---")
	}
	e := &Entry{}
	if err := yaml.Unmarshal([]byte(rest[:end]), e); err != nil {
		return nil, fmt.Errorf("解析 frontmatter: %w", err)
	}
	if e.Title == "" {
		return nil, fmt.Errorf("缺少 title")
	}
	if !ValidType(e.Type) {
		return nil, fmt.Errorf("非法 type %q（rule|pitfall|note|reference）", e.Type)
	}
	e.Body = strings.TrimSpace(rest[end+len(delim):])
	return e, nil
}

// StripFrontmatter 剥离内容开头的 "---" 分隔 front matter 块（与 Parse 同口径：
// 容忍 BOM 与 CRLF），返回剥离后的正文；无 front matter 时原样返回且 ok=false。
// 供 ok add --file 等外部正文入口使用，避免把源文件的 front matter 当正文导致
// Serialize 再包一层 --- 形成嵌套。
func StripFrontmatter(content []byte) (body []byte, ok bool) {
	s := strings.TrimPrefix(string(content), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return content, false
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	delim := "\n---\n"
	if end < 0 && strings.HasSuffix(rest, "\n---") {
		end = len(rest) - len("\n---")
		delim = "\n---"
	}
	if end < 0 {
		return content, false
	}
	return []byte(rest[end+len(delim):]), true
}

// Serialize 序列化为 "---\n<yaml>\n---\n\n<body>\n" 格式。
// Entry 全字段均为 string/bool/[]string，yaml.Marshal 实际不可失败，
// 但库代码不 panic，错误仍按常规返回。
func (e *Entry) Serialize() ([]byte, error) {
	fm, err := yaml.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("序列化 frontmatter: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n\n")
	buf.WriteString(e.Body)
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// Load 读取目录下全部 .md 条目，按文件名排序。
func Load(dir string) ([]*Entry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var entries []*Entry
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		e, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		e.Path = m
		entries = append(entries, e)
	}
	return entries, nil
}

// LoadTolerant 与 Load 相同，但单个文件读取/解析失败时跳过并收集错误，
// 只返回成功解析的条目；供 hook 注入路径使用，避免一个坏文件禁用全部注入。
func LoadTolerant(dir string) (entries []*Entry, errs []error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, []error{err}
	}
	sort.Strings(matches)
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		e, err := Parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m, err))
			continue
		}
		e.Path = m
		entries = append(entries, e)
	}
	return entries, errs
}

// Slug 将标题转为安全文件名（不含扩展名）：剔除路径元字符与控制字符，
// Windows 保留设备名前缀 "_" 避让，并限长（Windows MAX_PATH 下给目录前缀留余量）。
// 全被剔除时返回空串——调用方须判空拒绝（GUI/CLI 建条目路径均已校验）。
func Slug(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, " ", "-")
	slug := strings.Map(func(r rune) rune {
		switch {
		// 控制字符（含换行）：进文件名后在 Explorer/git 下极难处理
		case r < 0x20 || r == 0x7f:
			return -1
		}
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return -1
		}
		return r
	}, title)
	// Windows 保留设备名大小写不敏感，且 "con.md" 这类带扩展形态同样被保留
	base := slug
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToLower(base) {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		slug = "_" + slug
	}
	if n := []rune(slug); len(n) > 80 {
		slug = string(n[:80])
	}
	return slug
}

// FileName 返回条目在磁盘上的文件名。
func (e *Entry) FileName() string {
	if e.Path != "" {
		return filepath.Base(e.Path)
	}
	return Slug(e.Title) + ".md"
}

// EmbedText 是计算 embedding 时使用的文本。
func (e *Entry) EmbedText() string {
	return e.Title + "\n" + e.Summary + "\n" + e.Body
}
