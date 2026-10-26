package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packPath 是「工程模板包」在仓库里的位置（模板以**用户模板**形式分发：
// 落在 <DataRoot>/templates.json 里，不改 defaults.go，因此不与并行会话的在途件冲突）。
const packPath = "../../../docs/quality-packs/工程模板包-v1.json"

// loadPack 读模板包；读不到直接判红（它是仓库资产，不是可选项）。
func loadPack(t *testing.T) []tpl {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(packPath))
	if err != nil {
		t.Fatalf("读不到工程模板包 %s: %v", packPath, err)
	}
	var pack []tpl
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatalf("模板包不是合法模板数组: %v", err)
	}
	return pack
}

// TestEngineeringPackTemplatesValidate 钉住模板包里的每条都能过 Validate、
// ID 不与内置冲突、后继模板都能解析 —— 否则导入后是「坏模板」，
// 表现是那条模板静默不生效或被评估直接拒（本仓最怕的隐身失败）。
func TestEngineeringPackTemplatesValidate(t *testing.T) {
	pack := loadPack(t)
	if len(pack) < 4 {
		t.Fatalf("模板包只有 %d 条（期望 >=4）—— 判据会空集全绿，先补内容", len(pack))
	}
	builtin := map[string]bool{}
	for _, b := range Defaults() {
		builtin[b.ID] = true
	}
	if len(builtin) == 0 {
		t.Fatal("Defaults() 为空 —— 解析面没跑起来，本判据不可信")
	}
	packIDs := map[string]bool{}
	for _, p := range pack {
		if builtin[p.ID] {
			t.Errorf("模板包 id %q 与内置模板重名：导入后会**覆盖**内置条目而不是新增", p.ID)
		}
		if packIDs[p.ID] {
			t.Errorf("模板包内 id 重复: %q", p.ID)
		}
		packIDs[p.ID] = true
	}
	checkedSucc := 0
	for _, p := range pack {
		if err := Validate(p); err != nil {
			t.Errorf("模板 %s 校验失败（导入即坏）: %v", p.ID, err)
			continue
		}
		if strings.TrimSpace(p.Domain) == "" {
			t.Errorf("模板 %s 没有 domain：选题池与白名单都挂不上去", p.ID)
			continue
		}
		if !p.Enabled {
			t.Errorf("模板 %s enabled=false：导入后不会进轮转", p.ID)
		}
		for _, s := range p.Chain.SuccessorTemplates {
			checkedSucc++
			if !builtin[s] && !packIDs[s] {
				t.Errorf("模板 %s 的后继 %q 既不在内置也不在包里 —— 链会断在这里", p.ID, s)
			}
		}
	}
	// 受理面哨兵：链边必须真的被检查过（否则「后继都能解析」是空话）。
	if checkedSucc < 3 {
		t.Fatalf("只检查到 %d 条链边（期望 >=3）—— 判据失去受理面", checkedSucc)
	}
}

// TestEngineeringPackCoversObjectiveAreas 钉住「目标点名的能力面」在包里有模板对应。
// 目标口径（2026-10-26）：HTML/CSS/JS 与 React/Vue、TS、工程化构建、性能调优、报错修复、可扩展高可用架构。
// 判法用「关键词必须在某条 prompt 里出现」，不用语义判断（语义不可机判，本仓在册教训）。
func TestEngineeringPackCoversObjectiveAreas(t *testing.T) {
	pack := loadPack(t)
	if len(pack) == 0 {
		t.Fatal("模板包为空")
	}
	joined := ""
	for _, p := range pack {
		joined += p.PromptTemplate + "\n" + p.Description + "\n"
	}
	for _, kw := range []string{"HTML", "CSS", "TypeScript", "构建", "性能", "报错", "可扩展", "高可用"} {
		if !strings.Contains(joined, kw) {
			t.Errorf("模板包覆盖缺 %q —— 目标点名的这一面没有模板承接", kw)
		}
	}
	// 至少一条要明确覆盖“前端”这个整面（不是零散关键词凑数）。
	frontend := 0
	for _, p := range pack {
		if strings.Contains(p.PromptTemplate, "前端") {
			frontend++
		}
	}
	if frontend == 0 {
		t.Error("没有任何模板覆盖「前端」整面")
	}
}

// TestEngineeringPackImportsAsUserTemplates 走真实导入路径（Registry.Open 读
// <DataRoot>/templates.json）：断言条数 = 内置 + 包，且包里每条都能被 Get 到。
// 这是「放对地方了吗」的判据 —— 模板包放错位置就等于没交付。
func TestEngineeringPackImportsAsUserTemplates(t *testing.T) {
	pack := loadPack(t)
	dir := t.TempDir()
	raw, err := os.ReadFile(filepath.FromSlash(packPath))
	if err != nil {
		t.Fatalf("读模板包失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates.json"), raw, 0o644); err != nil {
		t.Fatalf("写临时用户模板失败: %v", err)
	}
	reg, err := Open(filepath.Join(dir, "templates.json"))
	if err != nil {
		t.Fatalf("Registry.Open: %v", err)
	}
	got := len(reg.List())
	want := len(Defaults()) + len(pack)
	if got != want {
		t.Fatalf("导入后模板总数 = %d，期望 %d（内置 %d + 包 %d）", got, want, len(Defaults()), len(pack))
	}
	for _, p := range pack {
		gotTpl, ok := reg.Get(p.ID)
		if !ok {
			t.Errorf("导入后取不到 %q —— 用户模板覆盖/追加路径没生效", p.ID)
			continue
		}
		if gotTpl.Builtin {
			t.Errorf("%q 被标成 builtin=true：用户模板不得冒充内置（会误导「恢复默认」语义）", p.ID)
		}
		if !gotTpl.Enabled {
			t.Errorf("%q 导入后 enabled=false：不会进轮转", p.ID)
		}
	}
}
