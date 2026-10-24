package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// WriteFileAtomic 以 tmp -> fsync -> rename 的方式原子写文件，保证断电/崩溃后
// 目标文件要么是旧内容、要么是新内容，绝不出现半截 JSON。
//
// 2026-10-25 修复兜底路径的数据归零缺陷（对标 prometheus tsdb/wlog 的 Repair 时发现）：
// 原实现在第一段 rename 失败后走「os.Remove(path) 再重试 rename」。
// 该窗口下若第二段 rename 也失败，函数返回错误，但**原文件已被删掉、新内容也没进去**
// => 目标文件彻底丢失。已由 seam 判据钉死（atomic_fallback_test.go 的 FallbackMatrix）。
//
// 修法：把「先删后建」改成「先备份后替换」——
//
//	rename 失败 => 把原文件 rename 成 <path>.bak（保留原数据）=> 再 rename tmp -> path。
//	任一步失败都能从 .bak 恢复；且 .bak 与原文件同目录，不引入新的失败面。
//
// 若连备份都失败（原文件被独占），则保留原文件不动，只清理 tmp 并返回错误
// —— 「写不进去」远好于「把旧数据删了还是写不进去」。
func WriteFileAtomic(path string, data []byte) error {
	return writeFileAtomicWith(path, data, os.Rename, os.Remove)
}

// fsRename / fsRemove 是文件系统 seam：默认真实实现，测试可注入失败。
//
// 为什么需要 seam（2026-10-25）：兜底路径的「备份成功但替换失败」窗口
// 在 Windows 上极难稳定构造 —— 实测扫过四种方案（独占打开 / 只读属性 /
// 空目录 / 非空目录）都构造不出「第一段失败但备份成功」的稳定组合。
// 没有 seam 就只能碰运气，那样的测试在别的机器上会静默 SKIP，
// 判据等于不存在（本仓在册的「vacuous pass」家族）。
var (
	fsRename = os.Rename
	fsRemove = os.Remove
)

// pathLocks 把「同一目标路径的原子写」串行化（2026-10-25 CI 实测暴露的缺陷）。
//
// 缺陷与取证：tmp 名唯一只解决了「临时文件互相覆盖」，目标与 .bak 仍是共享名。
// 四个 writer 并发写同一 path 时 Windows 报：
//
//	替换目标文件失败（已从备份恢复）: rename ... 被另一进程占用
//
// 即 rename(path 到 .bak) 与 rename(tmp 到 path) 在 writer 之间交错 ——
// A 把文件搬走的同时 B 正动同一目标，最终四个 writer 全部失败。
// 现场 CI run 40127263998：纯净检出 go test 判红（主 race job 绿，时序相关 = flaky）。
//
// 锁序复核（在册教训：加锁前必看锁序，防自锁/死锁）：持锁期间只做文件系统调用，
// 不再取本包或他包任何锁；外部调用方是 config.Store 的 s.mu 指向本锁的单向顺序，
// 反向不存在，无环。
//
// 为什么不给 .bak 也加唯一名：那样四个 writer 仍在抢目标，失败只是换个形式；
// 串行化一次同时解决目标与 .bak 两处竞态，且不同路径互不阻塞（key 取 path）。
//
// 内存边界（我自己引入的取舍，如实记）：pathLocks 不清理条目，每个被写过的不同
// path 留一个 mutex（约 100-200B）。增长由「不同路径数」而非「写次数」决定：
// queue/state/config 是固定名，导出文件名带时间戳（用户触发的低频操作，实测日增量级）。
// 不做清理的原因不是省事 —— 清理有正确性风险：条目在被持锁时删除，会让后续
// LoadOrStore 拿到第二把锁，等于把竞态换了个地方复现。宁留上界明确的小内存。
//
// 边界如实声明：key 区分大小写，而 Windows 文件系统不区分 —— 同一文件若被写成
// 两种大小写形式仍可能各拿一把锁。本仓路径全部由 config.BuildLayout 单点派生，
// 不存在第二种写法；为不存在的情形加平台分支归一反而引入新风险面，故不做。
var pathLocks sync.Map

// lockFor 取该路径的互斥锁并加锁，调用方负责 Unlock。
// key 经 filepath.Clean 归一化，避免同一文件的多种等价写法各拿一把锁。
func lockFor(path string) *sync.Mutex {
	v, _ := pathLocks.LoadOrStore(filepath.Clean(path), new(sync.Mutex))
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu
}

// writeFileAtomicWith 是带 seam 的实现主体（WriteFileAtomic 走默认 seam）。
func writeFileAtomicWith(path string, data []byte, rename func(old, new string) error, remove func(name string) error) error {
	// 同一目标路径串行化（原因与锁序复核见 pathLocks 注释）。
	defer lockFor(path).Unlock()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建父目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// cleanupTmp 只删临时文件；原文件的处置由下面的显式分支负责（绝不在此处删原文件）。
	cleanupTmp := func() {
		if tmp != nil {
			_ = tmp.Close()
		}
		_ = remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanupTmp()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanupTmp()
		return fmt.Errorf("fsync 失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanupTmp()
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	tmp = nil

	if err := rename(tmpName, path); err != nil {
		// 第一段 rename 失败（Windows 上目标被占用时常见）。
		// 关键：不能先删原文件。先把原文件备份，再替换。
		// 备份本身也失败（例如原文件被独占）=> 原数据不动，只报错。
		backup := path + ".bak"
		// 备份本身也失败（例如原文件被独占）=> 原数据不动，只报错。
		if berr := rename(path, backup); berr != nil {
			cleanupTmp()
			// 原文件不存在时不算失败：本就没有要保留的数据。
			if os.IsNotExist(berr) {
				if err2 := rename(tmpName, path); err2 != nil {
					return fmt.Errorf("重命名失败: %w", err2)
				}
				return nil
			}
			return fmt.Errorf("备份原文件失败（原数据保留未动）: %w", berr)
		}
		if err2 := rename(tmpName, path); err2 != nil {
			// 替换失败：从备份恢复原文件，确保数据不丢。
			_ = rename(backup, path)
			cleanupTmp()
			return fmt.Errorf("替换目标文件失败（已从备份恢复）: %w", err2)
		}
		// 替换成功：备份已无用，删不掉也无害（它只是残留副本）。
		_ = remove(backup)
	}
	return nil
}

// WriteJSONAtomic 原子写入缩进 JSON。
func WriteJSONAtomic(path string, v any) error {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	buf = append(buf, '\n')
	return WriteFileAtomic(path, buf)
}

// ReadJSON 读取 JSON；文件不存在时返回 os.ErrNotExist 供调用方判断。
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// 剥掉 UTF-8 / UTF-16 BOM（Windows 记事本/PowerShell 会写 UTF-8 BOM，
	// Go 的 json 会报 invalid character 从而让工作台启动即退出）。
	data = stripBOM(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", filepath.Base(path), err)
	}
	return nil
}

// AppendJSONL 以 O_APPEND 追加一条 JSON 记录（单行）。并发安全。
type JSONLWriter struct {
	mu   sync.Mutex
	path string
}

// NewJSONLWriter 构造追加写入器。
func NewJSONLWriter(path string) *JSONLWriter { return &JSONLWriter{path: path} }

// Path 返回底层文件路径。
func (w *JSONLWriter) Path() string { return w.path }

// Append 追加一条记录；序列化失败时报错且不写入。
func (w *JSONLWriter) Append(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("序列化记录失败: %w", err)
	}
	buf = append(buf, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志失败: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("写入日志失败: %w", err)
	}
	return nil
}

// stripBOM 剥掉常见 BOM 前缀（UTF-8 三字节 / UTF-16 LE / BE 两字节）。
func stripBOM(b []byte) []byte {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return b[3:]
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return b[2:]
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return b[2:]
	}
	return b
}
