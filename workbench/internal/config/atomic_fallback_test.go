package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// 本文件守护 WriteFileAtomic 兜底路径的「数据归零」缺陷修复（2026-10-25）。
//
// 缺陷来源（对标 prometheus tsdb/wlog 的 Repair 时发现）：
// 原实现在第一段 rename 失败后走「os.Remove(path) 再重试 rename」。
// 该窗口下若第二段 rename 也失败，函数返回错误，但原文件已被删掉、
// 新内容也没进去 => 目标文件彻底丢失。已用等效步骤实测复现过。
//
// 修法：「先删后建」改为「先备份后替换」——
//   rename 失败 => 先把原文件 rename 成 <path>.bak（保数据）=> 再替换；
//   替换也失败 => 从 .bak 恢复；备份就失败 => 原文件不动只报错。
//
// 判据分两层（第一层是主力，它跨平台确定生效）：
//   1) TestWriteFileAtomicFallbackMatrix —— seam 注入，把四种成败组合钉死；
//   2) TestWriteFileAtomicRenameFallbackKeepsData —— Windows 真实独占打开，
//      走「备份也失败 => 原数据保留」分支（Linux 上 rename 不拦，断言自然不触发，
//      所以它只能作补充，不能当唯一判据）。

// TestWriteFileAtomicRenameFallbackKeepsData 用真实文件系统验证：
// 目标被独占打开时（Windows），写失败后原数据必须还在。
func TestWriteFileAtomicRenameFallbackKeepsData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	const original = "ORIGINAL"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Windows：以读写方式独占打开目标，使 rename 失败。
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Skip("无法独占打开目标文件（平台不支持），跳过")
	}
	defer f.Close()

	werr := WriteFileAtomic(path, []byte("NEW"))
	t.Logf("WriteFileAtomic err (target locked): %v", werr)

	b, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("写失败后原文件消失（数据归零）: %v", rerr)
	}
	if string(b) != original {
		t.Fatalf("写失败后原内容被改成 %q，期望 %q", string(b), original)
	}
}

// TestWriteFileAtomicFallbackMatrix 用 seam 精确控制每一步的成败，
// 把「兜底路径」的全部组合钉死 —— 平台无关、不碰运气。
//
// 组合（第一段 rename / 备份 rename / 第二段 rename）：
//  1. 全成功：happy path
//  2. 败/成/成：走备份分支最终写成功，.bak 必须被清理
//  3. 败/成/败：备份成功但替换失败 => 必须从 .bak 恢复，原数据不丢
//  4. 败/败/-  ：备份也失败 => 原数据原样保留，且 tmp 必须清掉
//
// 变异实测：把实现改回「先删后建」，第 3/4 组同时判红
// （报「原文件消失」）—— 这是旧实现下必然出现的真实回退。
func TestWriteFileAtomicFallbackMatrix(t *testing.T) {
	const original = "ORIGINAL_DATA"

	setup := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, "queue.json")
		if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mustContent := func(t *testing.T, p, want string) {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读取 %s 失败（数据归零？）: %v", filepath.Base(p), err)
		}
		if string(b) != want {
			t.Fatalf("%s 内容 %q，期望 %q", filepath.Base(p), string(b), want)
		}
	}
	mustNotExist := func(t *testing.T, p string) {
		t.Helper()
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s 不应存在", filepath.Base(p))
		}
	}

	t.Run("happy-path", func(t *testing.T) {
		p := setup(t)
		if err := writeFileAtomicWith(p, []byte("NEW"), os.Rename, os.Remove); err != nil {
			t.Fatalf("默认 seam 不应报错: %v", err)
		}
		mustContent(t, p, "NEW")
		mustNotExist(t, p+".bak")
	})

	t.Run("backup-then-replace-succeeds", func(t *testing.T) {
		p := setup(t)
		// 第一段与第二段 rename 的参数完全相同（tmpName -> path），
		// 只能按调用次序区分。本场景序列：
		//   1) tmp -> path（第一段，要拦）2) path -> .bak（备份）3) tmp -> path（第二段）
		calls := 0
		err := writeFileAtomicWith(p, []byte("NEW"), func(o, n string) error {
			calls++
			if calls == 1 {
				return fmt.Errorf("first rename blocked")
			}
			return os.Rename(o, n)
		}, os.Remove)
		if err != nil {
			t.Fatalf("备份+替换成功路径不应报错: %v", err)
		}
		mustContent(t, p, "NEW")
		mustNotExist(t, p+".bak")
		if calls != 3 {
			t.Fatalf("rename 调用次序不符（期望 3 次，实际 %d）—— 实现结构变了须同步本测试", calls)
		}
	})

	t.Run("replace-fails-restores-from-backup", func(t *testing.T) {
		p := setup(t)
		// 序列：1) tmp->path 失败 2) path->.bak 成功 3) tmp->path 再失败 4) .bak->path 恢复
		calls := 0
		err := writeFileAtomicWith(p, []byte("NEW"), func(o, n string) error {
			calls++
			if calls == 2 || calls == 4 {
				return os.Rename(o, n) // 备份 / 恢复：放行
			}
			return fmt.Errorf("rename blocked: %s -> %s", o, n)
		}, os.Remove)
		if err == nil {
			t.Fatal("替换失败必须返回错误")
		}
		mustContent(t, p, original)
		mustNotExist(t, p+".bak")
		if calls != 4 {
			t.Fatalf("rename 调用次序不符（期望 4 次，实际 %d）", calls)
		}
	})

	t.Run("backup-fails-keeps-original-untouched", func(t *testing.T) {
		p := setup(t)
		err := writeFileAtomicWith(p, []byte("NEW"), func(o, n string) error {
			return fmt.Errorf("all rename blocked")
		}, os.Remove)
		if err == nil {
			t.Fatal("备份失败必须返回错误")
		}
		mustContent(t, p, original)
		mustNotExist(t, p+".bak")
		// tmp 不得残留
		entries, _ := os.ReadDir(filepath.Dir(p))
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".tmp" {
				t.Fatalf("临时文件未清理: %s", e.Name())
			}
		}
	})

	t.Run("missing-original-writes-fresh", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "fresh.json")
		if err := writeFileAtomicWith(p, []byte("NEW"), os.Rename, os.Remove); err != nil {
			t.Fatalf("新文件写入不应报错: %v", err)
		}
		mustContent(t, p, "NEW")
		mustNotExist(t, p+".bak")
	})
}
