package modelpool

import "testing"

// TestPreferFirst 钉住「链内重排」的口径（T17①）。
func TestPreferFirst(t *testing.T) {
	cases := []struct {
		name      string
		order     []string
		preferred string
		want      []string
	}{
		{"链尾提到首位", []string{"a", "b", "c", "d"}, "c", []string{"c", "a", "b", "d"}},
		{"已在首位不动", []string{"a", "b"}, "a", []string{"a", "b"}},
		{"不在链中不动", []string{"a", "b"}, "z", []string{"a", "b"}},
		{"空 preferred 不动", []string{"a", "b"}, "", []string{"a", "b"}},
		{"单元素不动", []string{"a"}, "a", []string{"a"}},
	}
	for _, c := range cases {
		got := preferFirst(c.order, c.preferred)
		if len(got) != len(c.want) {
			t.Fatalf("%s: 长度变了 %v → %v", c.name, c.order, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: preferFirst(%v, %q) = %v, want %v", c.name, c.order, c.preferred, got, c.want)
				break
			}
		}
	}
	// 不新增候选：链外模型不得被拉进来。
	if got := preferFirst([]string{"a", "b"}, "outside"); len(got) != 2 {
		t.Errorf("链外 preferred 不得新增候选，实得 %v", got)
	}
}

// TestLastOKModel 钉住「最近一次成功」的记录与读取。
func TestLastOKModel(t *testing.T) {
	m, _ := newTestManager(t)
	if _, _, ok := m.LastOKModel(); ok {
		t.Fatal("初始不应有 lastOK")
	}
	m.MarkSuccess("a", 1200)
	id, at, ok := m.LastOKModel()
	if !ok || id != "a" || at <= 0 {
		t.Fatalf("MarkSuccess 后应为 a 且带时刻，实得 (%q,%d,%v)", id, at, ok)
	}
	m.MarkSuccess("b", 900)
	if id, _, _ := m.LastOKModel(); id != "b" {
		t.Fatalf("最近成功应更新为 b，实得 %q", id)
	}
	m.MarkFailure("c", "stalled", "零进展")
	if id, _, _ := m.LastOKModel(); id != "b" {
		t.Fatalf("失败不得覆盖 lastOK（仍应为 b），实得 %q", id)
	}
}
