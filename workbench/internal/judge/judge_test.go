package judge

import (
	"strings"
	"testing"

	"workbench/internal/executor"
)

// judge 包此前 0 覆盖（issue #3）：语义层是"自评→链式衍生"的唯一输入，
// 解析判坏了会让 7×24 无人值守产出静默劣化。本文件锁住三个解析纯函数，
// 全部含负面用例（空输入 / 超长 / 非法 JSON / 字段缺失）。

// ---------- extractJSON ----------

func TestExtractJSONShapes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"裸对象", `{"a":1}`, `{"a":1}`},
		{"裸数组", `[{"a":1}]`, `[{"a":1}]`},
		{"前后废话", "好的，结果如下：\n" + `{"quality":8}` + "\n以上。", `{"quality":8}`},
		{"嵌套平衡", `{"a":{"b":[1,2,{"c":3}]}}`, `{"a":{"b":[1,2,{"c":3}]}}`},
		{"字符串内含花括号", `{"s":"{不是结束}"}`, `{"s":"{不是结束}"}`},
		{"转义引号", `{"s":"he said \\"x\\""}`, `{"s":"he said \\"x\\""}`},
		{"无 JSON", "纯文本没有花括号", ""},
		{"空串", "", ""},
		{"未闭合", `{"a":1`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractJSON(c.in); got != c.want {
				t.Fatalf("extractJSON(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
	// 围栏包裹（模型常带 json 代码围栏前后缀）
	fence := "答案：\n```json\n{\"quality\":7}\n```\n"
	if got := extractJSON(fence); !strings.Contains(got, "quality") {
		t.Fatalf("围栏 JSON 未提取: %q", got)
	}
}

// ---------- parseJudgeOutput ----------

func TestParseJudgeOutputOK(t *testing.T) {
	res := &executor.Result{FinalText: `[{"id":"t1","value":85,"feasibility":90,"reason":"可落地"},
		{"id":"t2","value":20,"feasibility":30,"reason":"重复"}]`}
	got, err := parseJudgeOutput(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("结果数 = %d, want 2", len(got))
	}
	if got["t1"].Value != 85 || got["t1"].Feasibility != 90 {
		t.Fatalf("t1 分数错误: %+v", got["t1"])
	}
}

func TestParseJudgeOutputNegatives(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"无 JSON", "模型今天不想打分"},
		{"空数组", "[]"},
		{"全空 id", `[{"id":"","value":1}]`},
		{"非法 JSON", `[{"id":"t1",broken]`},
		{"对象而非数组", `{"id":"t1","value":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseJudgeOutput(&executor.Result{FinalText: c.text}); err == nil {
				t.Fatalf("%s 必须报错（fail-open 依赖 error 非空）", c.name)
			}
		})
	}
	// 混合：一条有 id 一条没有 → 保留有效条，不因脏条整体失败
	got, err := parseJudgeOutput(&executor.Result{FinalText: `[{"id":"","value":9},{"id":"ok","value":70}]`})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["ok"].Value != 70 {
		t.Fatalf("应只保留有效条: %+v", got)
	}
}

// ---------- parseCritique ----------

func TestParseCritiqueClampsAndFilters(t *testing.T) {
	// quality 越界必须钳位到 [0,10]（下游 MinQuality 判定依赖它）
	high := &executor.Result{FinalText: `{"quality":42,"tags":[],"strengths":[],"gaps":[]}`}
	cq, err := parseCritique(high)
	if err != nil {
		t.Fatal(err)
	}
	if cq.Quality != 10 {
		t.Fatalf("quality 上钳位失败: %d", cq.Quality)
	}
	low := &executor.Result{FinalText: `{"quality":-5}`}
	cq, err = parseCritique(low)
	if err != nil {
		t.Fatal(err)
	}
	if cq.Quality != 0 {
		t.Fatalf("quality 下钳位失败: %d", cq.Quality)
	}

	// nextIdeas：空标题/空 prompt 条目必须丢弃，最多保留 2 条
	res := &executor.Result{FinalText: `{"quality":8,"nextIdeas":[
		{"title":"","prompt":"x"},
		{"title":"ok1","prompt":""},
		{"title":"ok2","prompt":"产出A","domain":"code"},
		{"title":"ok3","prompt":"产出B"},
		{"title":"ok4","prompt":"产出C"}]}`}
	cq, err = parseCritique(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(cq.NextIdeas) != 2 {
		t.Fatalf("nextIdeas 应截断到 2 条，实际 %d", len(cq.NextIdeas))
	}
	if cq.NextIdeas[0].Title != "ok2" || cq.NextIdeas[0].Domain != "code" {
		t.Fatalf("首条应是第一个合法 idea: %+v", cq.NextIdeas[0])
	}
}

func TestParseCritiqueNegatives(t *testing.T) {
	for _, text := range []string{"", "没有 JSON", `{"quality":"不是数字"}`, `[1,2]`} {
		if _, err := parseCritique(&executor.Result{FinalText: text}); err == nil {
			t.Fatalf("非法自评必须报错: %q", text)
		}
	}
}

// ---------- 超长输入压力（负面测试：模型输出不可信长度）----------

func TestParsersSurviveOversizedOutput(t *testing.T) {
	huge := strings.Repeat("废话", 200000) + `{"quality":6,"tags":[],"strengths":[],"gaps":[]}`
	cq, err := parseCritique(&executor.Result{FinalText: huge})
	if err != nil {
		t.Fatalf("40 万字符前缀废话应仍能解析: %v", err)
	}
	if cq.Quality != 6 {
		t.Fatalf("quality = %d, want 6", cq.Quality)
	}
}
