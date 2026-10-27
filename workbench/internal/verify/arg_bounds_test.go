package verify

import "testing"

// TestArgOutOfBounds 钉住「防越界」判据的边界（round 45 · T30）。
//
// 由来：原判据用 `strings.Contains(a, "..")` 一律拒绝 ⇒ `go test ./...` 这类**合法**验收命令被误杀，
// 每次验收都落 skippedReason、任务白白判失败。正确口径是「Clean 之后仍越出 workspace 才算越界」。
func TestArgOutOfBounds(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string // 期望被判越界的那个参数（"" = 全部放行）
	}{
		{"go test ./...（最典型的验收命令）", []string{"./..."}, ""},
		{"递归点号两种写法", []string{"./...", "..."}, ""},
		{"包路径", []string{"./internal/..."}, ""},
		{"a/../b 规范化后仍在内部", []string{"a/../b"}, ""},
		{"普通参数", []string{"test", "-run", "TestX"}, ""},
		{"上越一级", []string{"../../etc/passwd"}, "../../etc/passwd"},
		{"绝对路径", []string{"/etc/passwd"}, "/etc/passwd"},
		{"Windows 绝对路径", []string{"C:/Windows/x"}, "C:/Windows/x"},
		{"混合里有一个越界：只报那一个", []string{"./...", "../../x"}, "../../x"},
	}
	for _, c := range cases {
		if got := argOutOfBounds(c.args); got != c.want {
			t.Errorf("%s: argOutOfBounds(%v) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}
