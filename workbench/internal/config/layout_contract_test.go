package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// layoutClasses 声明 Layout 每个字符串字段应当挂在哪个根之下。
// root = 便携根，data = DataRoot（workbench-output），runtime = RuntimeDir。
var layoutClasses = map[string]string{
	"Root":               "root",
	"DataRoot":           "root",
	"ConfigFile":         "data",
	"QueueFile":          "data",
	"StateFile":          "data",
	"WALFile":            "data",
	"TasksDir":           "data",
	"LogsDir":            "data",
	"ExportsDir":         "data",
	"GovDir":             "data",
	"AutonomyDir":        "data",
	"WorkspaceAuditFile": "data",
	"RuntimeDir":         "root",
	"DSHHome":            "runtime",
}

// TestBuildLayoutContract 钉住 BuildLayout 的三条布局契约（CONTRIBUTING 硬约束 #6 与结构体注释的承诺）。
//
// 为什么必须有测试（2026-10-25 实测）：config 包 9 个受跟踪测试文件里，BuildLayout / DataRoot /
// workbench-output 三个词出现 0 次 —— 而硬约束 #6 写着「产出根固定 workbench-output，
// 面板/导出/恢复逻辑都基于该布局」。今天它只靠 paths.go 里一行 Join(root, "workbench-output")
// 成立：改那一行不会让任何测试变红，但会让 10 个派生路径整体搬家（面板读不到 queue.json、
// 导出目录消失、断点恢复找不到 state.json）—— 一处改动、全系统静默错位。
//
// 判据分两层（这个分层是被变异测试逼出来的，不是设计出来的）：
//
//	反射遍历字段 -> 保证「新增字段必须显式归类」，否则判据覆盖会静默缩小；
//	显式归类映射 -> 保证「字段被改挂到别的根」能被发现。
//
// 反面教材（同一轮实测）：我第一版把归类映射删掉、改成「落在三大根之一即可」，
// 于是「TasksDir 从 DataRoot 改挂到 Root」直接漏过（root 下的 tasks 当然有 root 前缀）。
// 放宽判据 = 制造假绿，与 round 64/99 在册的是同一类。
func TestBuildLayoutContract(t *testing.T) {
	root := filepath.Join(t.TempDir(), "portable")
	l, err := BuildLayout(root, "")
	if err != nil {
		t.Fatalf("BuildLayout 失败: %v", err)
	}

	// 判据 1：数据根的名字必须是 workbench-output（硬约束 #6）
	if base := filepath.Base(l.DataRoot); base != "workbench-output" {
		t.Errorf("DataRoot 尾部 = %q，必须是 workbench-output（面板/导出/恢复都基于该布局）", base)
	}
	if want := filepath.Join(root, "workbench-output"); l.DataRoot != want {
		t.Errorf("DataRoot = %q，应为 root 下的 workbench-output", l.DataRoot)
	}

	// 判据 2：每个字段按 layoutClasses 归位（反射遍历 + 显式归类）
	prefixOf := map[string]string{
		"root":    root,
		"data":    l.DataRoot,
		"runtime": l.RuntimeDir,
	}
	rv := reflect.ValueOf(l)
	seen := map[string]bool{}
	for i := 0; i < rv.NumField(); i++ {
		name := rv.Type().Field(i).Name
		if rv.Type().Field(i).Type.Kind() != reflect.String {
			continue
		}
		v := rv.Field(i).String()
		seen[name] = true
		cls, ok := layoutClasses[name]
		if !ok {
			t.Errorf("Layout.%s 未在 layoutClasses 归类 —— 新增路径字段必须显式声明它挂在哪个根之下", name)
			continue
		}
		if v == "" {
			t.Errorf("Layout.%s 为空 —— 布局字段未派生", name)
			continue
		}
		pre := prefixOf[cls]
		// Root 字段本身等于 root；其余必须是「某根之下」而非等于根
		if v != pre && !strings.HasPrefix(v, pre+string(filepath.Separator)) {
			t.Errorf("Layout.%s = %q 不在其归类 %q 所指根（%q）之下 —— 该路径被改挂别处，读它的子系统会静默找不到文件", name, v, cls, pre)
		}
	}
	// 反向：映射里声明了但结构体没有的字段（改名/删除也要显式改这里）
	for name := range layoutClasses {
		if !seen[name] {
			t.Errorf("layoutClasses 含 %q 但 Layout 已无此字段 —— 结构改了须同步本清单", name)
		}
	}

	// 判据 3：DSHHome 是隔离 profile 根，绝不落在用户 ~/.dsh 下（硬约束 #2 同源）
	if want := filepath.Join(l.RuntimeDir, "dsh-home"); l.DSHHome != want {
		t.Errorf("DSHHome = %q，应为 RuntimeDir 下的 dsh-home", l.DSHHome)
	}
	if home, err := os.UserHomeDir(); err == nil {
		guard := filepath.Join(home, ".dsh") + string(filepath.Separator)
		if strings.HasPrefix(l.DSHHome, guard) {
			t.Errorf("DSHHome = %q 落在用户 ~/.dsh 之下 —— 违反硬约束 #2 与「绝不污染用户 profile」的承诺", l.DSHHome)
		}
	}
}
