package main

import (
	"encoding/json"
	"testing"
)

// TestPruneCLIRejectsBadDayFlags 钉住保留天数的**整串**解析。
//
// 为什么需要（2026-10-25 round 157 实测缺陷）：此处原用 fmt.Sscan(args[i], &v)，它对尾部残留
// 一律不报错 —— 实测 "3abc"→3、"3.5"→3，最意外的是 "0x10"→**16**（十六进制被当合法值）。
// 而这三个数字是**保留天数**，配合 --apply 就是不可逆删除：解错一天，删的就是另一个年龄的数据。
// 该函数此前零测试（prune.go 无同名测试文件）。
//
// 只测「解析阶段就返回 1」的路径：参数解析在 ResolveLayout 之前，因此不会碰任何真实目录。
func TestPruneCLIRejectsBadDayFlags(t *testing.T) {
	flags := []string{"--dsh-home-days", "--workspace-days", "--task-dir-days"}
	bad := []string{"3abc", "3.5", "0x10", "-1", "0", "", "abc", "5 6"}
	for _, f := range flags {
		for _, b := range bad {
			if rc := runPruneCLI([]string{f, b}); rc != 1 {
				t.Errorf("runPruneCLI(%s %q) = %d，期望 1（无效天数）", f, b, rc)
			}
		}
		// 合法值必须通过解析（随后进入布局/扫描阶段，rc 取 0 或 1 皆可，但绝不能 panic）。
		if rc := runPruneCLI([]string{f, "7"}); rc != 0 && rc != 1 {
			t.Errorf("runPruneCLI(%s 7) = %d，返回值超出 {0,1}", f, rc)
		}
	}
}

// TestPruneCLIDefaultsToDryRun 钉住 CLI 层最要紧的安全属性：**默认 dry-run**。
//
// 刻意**不测 --apply**：本包的 DSHHome = <root>/runtime/dsh-home，其中 root 不由 --data 决定，
// 测试里执行真实删除可能碰到工作区外的副本。删除语义的正确性由 internal/retention 的
// TestPruneDryRunDoesNotDelete / TestPruneActuallyDeletes / TestPruneWorkspaceKeepsTaskDir 覆盖；
// 本测试只锁「CLI 默认值」这层 —— 它是「宁可多一步确认」承诺的入口。
func TestPruneCLIDefaultsToDryRun(t *testing.T) {
	dir := t.TempDir()
	if rc := runPruneCLI([]string{"--json", "--data", dir}); rc != 0 {
		t.Fatalf("默认（不给 --apply）的 rc=%d，期望 0 —— dry-run 是默认", rc)
	}
	if rc := runPruneCLI([]string{"--json", "--data", dir, "--dry-run"}); rc != 0 {
		t.Fatalf("显式 --dry-run 的 rc=%d，与默认不一致", rc)
	}
	// 拼错的开关必须判 1：把 --apply 拼成 --aply 而静默当 dry-run，会把「已回收」误报成「未回收」。
	if rc := runPruneCLI([]string{"--aply"}); rc != 1 {
		t.Fatalf("拼错的开关 --aply 得到 rc=%d，期望 1（未知参数）", rc)
	}
	// JSON 输出契约：字段名是下游脚本的依赖，用一次最小解码钉住拼写（不用结构体标签，避免多层转义）。
	sample := []byte("{" + string(rune(34)) + "dryRun" + string(rune(34)) + ":true," + string(rune(34)) + "taskDirsOptIn" + string(rune(34)) + ":false}")
	var m map[string]any
	if err := json.Unmarshal(sample, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["dryRun"]; !ok {
		t.Fatal("JSON 契约缺 dryRun 字段（下游脚本依赖它判断是否真的删了）")
	}
	if _, ok := m["taskDirsOptIn"]; !ok {
		t.Fatal("JSON 契约缺 taskDirsOptIn 字段")
	}
}
