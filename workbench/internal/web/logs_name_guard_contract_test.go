package web

// logs_name_guard_contract_test.go —— 守护「日志文件名不得含路径语义」与「任务详情只认已存在 id」（2026-11-02 新增）。
//
// 为什么需要（与 r258 的 sanitizeFileName 黑洞同族 —— 守门逻辑在、判据缺）：
//   · handleLogs 用 r.URL.Query().Get("file") 直接拼 layout.LogFile(name) 读盘。它已有两道防线：
//     strings.ContainsAny(name, 反斜杠 斜杠 冒号 星号 问号 引号 尖括号 竖线) 挡路径提供符、以及全局 guard 强制 token。
//     但**没有任何测试锁住这两道防线** —— 谁把 ContainsAny 那行删了、或放宽字符集，日志接口立刻变成任意文件读，
//     而现有判据一条都不会红。
//   · handleTaskDetail 的 rest/id 直接进 layout.TaskFile(id, "events.jsonl") 读盘，安全性依赖「先 Store().Get(id)
//     命中才继续」。这条顺序约束同样零测试：若有人把 Get 校验挪到读盘之后，id 就能带路径语义去拼 TaskFile。
//
// 断言口径：锁**行为**（非法名被拒、未知 id 不泄露文件内容），而不是锁源码文本 —— 实现可以换写法，行为不能变。

import (
	"net/http"
	"strings"
	"testing"
)

func TestLogsRejectsPathLikeFileNames(t *testing.T) {
	srv, _, _ := newTestServer(t)
	// 每一种都能把读写引到日志目录之外；十进制字符码避免源码里出现转义层。
	bs := string(rune(92))
	bad := []string{
		"../audit.jsonl",
		".." + bs + "audit.jsonl",
		"C:audit.jsonl",
		"a/b.jsonl",
		"a" + bs + "b.jsonl",
		"a*b.jsonl",
		"a?b.jsonl",
		"a|b.jsonl",
		"a<b.jsonl",
		"a>b.jsonl",
	}
	for _, name := range bad {
		code, out := doJSON(t, srv.handleLogs, srv, http.MethodGet, "/api/logs?file="+urlQuery(name), nil)
		// 真判据：非法名必须走到 bad() 分支 —— 那是 400。
		// 早期版本我把「200 且空列表」也当通过 ⇒ 短路掉 ContainsAny 校验时测试照样绿（假判据，2026-11-02 自纠）。
		// 「被拒绝」与「文件不存在」是两件事：前者必须 400，后者才是 200 空列表。
		if code != http.StatusBadRequest {
			t.Fatalf("文件名 %q 含路径语义，期望 400 拒绝，实得 %d（返回 %v）—— 任意文件读风险", name, code, out)
		}
	}

}
func TestTaskDetailUnknownIDDoesNotLeakFiles(t *testing.T) {
	srv, _, _ := newTestServer(t)
	bs := string(rune(92))
	ids := []string{
		"..",
		".." + bs + ".." + bs + "etc",
		"task-not-exist",
	}
	for _, id := range ids {
		code, out := doJSON(t, srv.handleTaskDetail, srv, http.MethodGet, "/api/tasks/"+urlQuery(id), nil)
		if code == http.StatusOK {
			// 未知 id 不得返回任务内容；若返回则必须明确 not found 语义
			if _, hasTask := out["task"]; hasTask {
				t.Fatalf("未知 id %q 返回了 task 内容 —— 说明未先校验存在性", id)
			}
			if _, hasEvents := out["events"]; hasEvents {
				t.Fatalf("未知 id %q 返回了 events 内容 —— 存在路径拼接读盘风险", id)
			}
		}
	}
}

// urlQuery 用最朴素的方式把参数塞进 query，不引入额外依赖；只用于本文件的固定样本。
func urlQuery(s string) string {
	return strings.ReplaceAll(s, " ", "%20")
}
