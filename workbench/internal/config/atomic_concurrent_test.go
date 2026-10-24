package config

// atomic_concurrent_test.go 钉住「同一目标路径并发原子写」的两条性质
// （2026-10-25 CI run 40127263998 暴露：四个并发 writer 写同一 path 时全部失败，
//  错误是 rename 层的「被另一进程占用」/「重命名失败」—— 主 race job 绿、
//  纯净检出 job 红，复现率实测约 1/30，是典型时序 flaky）。
//
// 性质一：全部成功 —— 同路径并发不应有 writer 失败（pathLocks 串行化保证）。
// 性质二：不出现撕裂内容 —— 落盘内容必须是**某一个** writer 的完整载荷，
//         不能是混合体。四个载荷等长、各由同一字节重复而成，混合一眼可辨。
//
// 为什么在 config 层测：writeDocx 只是撞它的下游（web 层那条并发测试正是 CI 里
// 报红的信号源）。锁加在 WriteFileAtomic 上，判据就该钉在它自己的包，
// 否则将来别的使用者踩同一坑时没人报警。
//
// 为什么不用 t.Parallel 而是轮内并发：要测的是**同一路径**的多 writer 竞争，
// 不是多路径互不阻塞（后者已由 key 取 path 的设计保证，无需重复测）。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteFileAtomicConcurrentSamePath(t *testing.T) {
	const writers = 4
	const rounds = 60 // 本机实测复现率约 1/30，60 轮足以在没有修复时高概率暴露
	path := filepath.Join(t.TempDir(), "shared.json")
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = []byte(strings.Repeat(fmt.Sprintf("%c", 'A'+i), 4096))
	}
	// 先落一个初值，让后续 writer 都面对「目标已存在」的真实形态
	if err := WriteFileAtomic(path, payloads[0]); err != nil {
		t.Fatalf("初值写入失败: %v", err)
	}
	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		errs := make([]error, writers)
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = WriteFileAtomic(path, payloads[i])
			}(i)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("第 %d 轮 writer %d 失败（同路径并发竞态，pathLocks 未生效？）: %v", r, i, err)
			}
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("第 %d 轮读回失败: %v", r, err)
		}
		known := false
		for _, p := range payloads {
			if string(got) == string(p) {
				known = true
				break
			}
		}
		if !known {
			t.Fatalf("第 %d 轮内容撕裂：既非任何单一 writer 的载荷（长度 %d，首字节 %q）", r, len(got), got[:1])
		}
	}
	// 无残渣：串行化后不应留下 .bak / .tmp（留下虽不算数据错误，但说明兜底路径被触发）
	for _, pat := range []string{"*.bak", "*.tmp"} {
		left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), pat))
		if len(left) > 0 {
			t.Errorf("留下残渣 %v（%s）—— 备份/临时路径被走到过，值得查", left, pat)
		}
	}
}
