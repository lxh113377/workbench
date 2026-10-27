package templates

import (
	"path/filepath"
	"testing"
)

// 本文件是 T8「内置模板转正（33 条）」的验收判据。为什么这么判：
//   - 目标口径是「33 条真的出现在**注册表**里」，所以断言走真实注册表实例（Open），
//     不用「数据里数一遍」这种自证式判据（那只能证明数据存在，证明不了可用）；
//   - 当前 defaults.go 是并行会话在途脏件（M），4 条不可能写进内置层，
//     注入只能走本包新增的两条通道：公开注册 API 与外部文件。判据把两条都钉住；
//   - 对「defaults.go 将来正式落地」兼容：若 4 条已进内置，注册 API 必须是幂等
//     no-op（返回 0），总数仍 33，不判红 —— 避免我们自己的判据变成第二块绊脚石。

func openTempRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := Open(filepath.Join(t.TempDir(), "templates.json"))
	if err != nil {
		t.Fatalf("Open 临时注册表失败: %v", err)
	}
	return reg
}

// TestPromotedPackRegistersIntoRegistry 走公开注册 API：Open 后补注册，
// 断言注册表恰含 33 条、4 条按**内置语义**（builtin=true）存在且链边可解析。
func TestPromotedPackRegistersIntoRegistry(t *testing.T) {
	reg := openTempRegistry(t)
	base := len(reg.List())
	if base < 20 {
		t.Fatalf("内置基线只有 %d 条（<20）—— Defaults() 解析面没跑起来，判据会空集全绿", base)
	}
	// 受理面哨兵：4 条必须真的还不在注册表里，否则这条判据测的是空气。
	missingBefore := 0
	for _, id := range PromotedPackIDs() {
		if _, ok := reg.Get(id); !ok {
			missingBefore++
		}
	}
	if missingBefore == 0 {
		t.Fatalf("4 条转正模板已全在注册表里 —— 本判据对「注册 API 是否生效」失去受理面")
	}
	added := reg.RegisterPromotedBuiltins()
	if added != missingBefore {
		t.Fatalf("注册 API 新增 %d 条，期望恰好补齐 %d 条", added, missingBefore)
	}
	if again := reg.RegisterPromotedBuiltins(); again != 0 {
		t.Fatalf("重复注册返回 %d（应为 0）—— 注册 API 不幂等", again)
	}
	all := reg.List()
	if len(all) != base+missingBefore {
		t.Fatalf("注册后总数 %d，期望 %d", len(all), base+missingBefore)
	}
	// 目标口径快照：现存 defaults.go = 29 条内置，补齐后必须**恰好 33 条**。
	if base == 29 && len(all) != 33 {
		t.Fatalf("目标口径未达成：内置 29 + 转正 4 应 = 33，实测 %d", len(all))
	}
	for _, id := range PromotedPackIDs() {
		got, ok := reg.Get(id)
		if !ok {
			t.Errorf("注册后取不到 %q —— 注入口没生效", id)
			continue
		}
		if err := Validate(got); err != nil {
			t.Errorf("%q 过不了 Validate（导入即坏模板）: %v", id, err)
		}
		if !got.Enabled {
			t.Errorf("%q enabled=false —— 不会进轮转", id)
		}
		if !got.Builtin {
			t.Errorf("%q builtin=false —— 没按内置语义注入（会落盘成用户模板）", id)
		}
	}
	// 链完整性：4 条的每个后继都必须能在「内置 ∪ 转正」并集里解析，
	// 否则链会在这一步静默停止延伸（本仓在册缺陷形态）。
	known := map[string]bool{}
	for _, x := range Defaults() {
		known[x.ID] = true
	}
	for _, x := range all {
		known[x.ID] = true
	}
	edges := 0
	for _, id := range PromotedPackIDs() {
		got, _ := reg.Get(id)
		for _, s := range got.Chain.SuccessorTemplates {
			edges++
			if !known[s] {
				t.Errorf("%q 的后继 %q 不在注册表里 —— 链会断在这里", id, s)
			}
		}
	}
	if edges == 0 {
		t.Fatalf("4 条转正模板的后继边为 0 —— 链配置整体丢失，判据在比空气")
	}
	t.Logf("转正后注册表 %d 条（内置 %d + 新增 %d）；4 条模板后继边 %d 条", len(all), base, added, edges)
}

// TestPromotedPackExternalFileIsLoadable 走外部文件通道：InstallPromotedPack 写
// <DataRoot>/templates.json，再由既有 Open() 读取 —— 证明**不改任何加载代码**
// 也能让这 4 条在真实启动路径（main.go:191 templates.Open）里生效。
func TestPromotedPackExternalFileIsLoadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	if err := InstallPromotedPack(path); err != nil {
		t.Fatalf("InstallPromotedPack: %v", err)
	}
	reg, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	builtin := map[string]bool{}
	for _, x := range Defaults() {
		builtin[x.ID] = true
	}
	missing := 0
	for _, id := range PromotedPackIDs() {
		if !builtin[id] {
			missing++
		}
	}
	all := reg.List()
	if len(all) != len(builtin)+missing {
		t.Fatalf("外部文件导入后总数 %d，期望 %d（内置 %d + 缺失 %d）", len(all), len(builtin)+missing, len(builtin), missing)
	}
	if len(builtin) == 29 && len(all) != 33 {
		t.Fatalf("目标口径未达成：外部文件路径应给到 33 条，实测 %d", len(all))
	}
	for _, id := range PromotedPackIDs() {
		got, ok := reg.Get(id)
		if !ok {
			t.Errorf("外部文件导入后取不到 %q", id)
			continue
		}
		if got.Builtin {
			t.Errorf("%q 被标 builtin=true —— 用户模板不得冒充内置（会误导恢复默认语义）", id)
		}
		if !got.Enabled {
			t.Errorf("%q 导入后 enabled=false —— 不会进轮转", id)
		}
	}
	t.Logf("外部文件路径：注册表 %d 条（内置 %d + 文件补齐 %d）", len(all), len(builtin), missing)
}
