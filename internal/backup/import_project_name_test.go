package backup

import (
	"bytes"
	"errors"
	"testing"

	"archive/zip"
)

// R3 E-02：条目/配置/wiki 路径的项目段（parts[1]）必须形状合法且在包内
// registry.toml 登记。畸形名（Windows 保留设备名）在 MkdirAll 即挂，
// "重新导入可续传"对该包永久失效；未登记名会写出孤儿目录并重建索引。
func TestImportRejectsBadProjectSegment(t *testing.T) {
	setupHome(t)
	good := []byte("---\ntitle: 好\ntype: note\ntags: []\nsummary: s\ndraft: false\nmandatory: false\n---\n正文\n")
	reg := []byte("[[project]]\nname = \"alpha\"\npaths = [\"D:/src/alpha\"]\n")

	cases := []struct {
		name string
		path string // projects/<seg>/... 里的问题段
	}{
		{"保留设备名", "con"},
		{"形状非法", "a/b"},
		{"未在包内注册表登记", "ghost"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("registry.toml")
		w.Write(reg)
		w2, _ := zw.Create("projects/alpha/knowledge/good.md")
		w2.Write(good)
		w3, _ := zw.Create("projects/" + c.path + "/knowledge/x.md")
		w3.Write(good)
		zw.Close()
		_, err := Import(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if !errors.Is(err, ErrBadPackage) {
			t.Fatalf("%s: 应拒绝导入（ErrBadPackage），got %v", c.name, err)
		}
	}
}
