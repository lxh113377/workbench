package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrationPersistedOnLoad 覆盖「迁移只改内存不落盘」的现场缺陷。
//
// 现场（2026-10-19 08:50）：config.json 里 schemaVersion 已是 3，
// 但 appendProtocol 仍是 false、repairRounds 仍是 0 —— 因为 Normalize()
// 只改内存，从不写回文件。每次重启都重新迁移，文件永远停在旧值。
func TestMigrationPersistedOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// 手写一份「旧配置」：v3 + P3 字段零值
	old := DefaultConfig()
	old.SchemaVersion = 3
	old.Executor.AppendProtocol = false
	old.Executor.RepairRounds = 0
	data, err := MarshalConfig(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// 打开即应完成迁移并落盘
	if _, err := NewStore(path); err != nil {
		t.Fatal(err)
	}

	var onDisk Config
	if err := ReadJSON(path, &onDisk); err != nil {
		t.Fatal(err)
	}
	if !onDisk.Executor.AppendProtocol {
		t.Fatal("迁移必须落盘：磁盘上的 appendProtocol 仍为 false")
	}
	if onDisk.Executor.RepairRounds <= 0 {
		t.Fatalf("迁移必须落盘：磁盘上的 repairRounds = %d", onDisk.Executor.RepairRounds)
	}
	if onDisk.SchemaVersion != SchemaVersion {
		t.Fatalf("磁盘 schemaVersion = %d, want %d", onDisk.SchemaVersion, SchemaVersion)
	}
}

// TestNoRewriteWhenAlreadyMigrated 已是最新配置时不应产生无谓写盘。
func TestNoRewriteWhenAlreadyMigrated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	fresh := DefaultConfig()
	fresh.UpdatedAt = 12345
	data, _ := MarshalConfig(fresh)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)

	if _, err := NewStore(path); err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	if err := ReadJSON(path, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.UpdatedAt != fresh.UpdatedAt {
		t.Fatalf("已迁移配置被无谓重写: updatedAt %d -> %d", fresh.UpdatedAt, onDisk.UpdatedAt)
	}
	_ = before
}
