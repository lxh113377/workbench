package engine

import "testing"

// TestReportGatePathForms 钉住报告内容门禁的**触发真值表**（round 151 探针实测后固化）。
//
// 两个谓词各自独立，混起来就会像我第一版那样得出错误期望：
//
//	· isReportArtifact(path)  = 路径是否 docs/ 下的 .md（**只看路径**）
//	· claimsReport(path,text) = 是否自称报告（小节标题 或 文件名含 报告/审查/review，**不看目录**）
//
// 内容门禁真正生效 = 两者**同时**为真（reportGateProblems 里先 isReportArtifact 再 claimsReport）。
//
// 为什么要钉这张表：探针实测把同一份不合格正文以 `./docs/报告.md` 传进去时报 0 个问题
// （isReportArtifact 按前缀匹配 ⇒ 整道门被跳过）。第一版探针我正是踩了这个形态，
// 把三份现场产物全读成"判合格"—— 空集读成 PASS，本会话登记过的老毛病第 4 次。
// ⇒ 现在由断言强制："进门"与"合格"必须是两件事，且跳过时不许冒充零问题为合格。
//
// 绕过面目前无台账证据：现场 23 份产物 / 4 份 .md 里，报告类交付**全部**是标准 docs/ 前缀，
// 唯一非标的 a.md 落在仓库根、已被落点门禁 T74 拦下 ⇒ 按"无实据不改行为"只钉现状，不扩大覆盖。
func TestReportGatePathForms(t *testing.T) {
	placeholder := "# 复盘\n\n> 状态：待填充\n"  // 自称报告形态、但无三节无分级：进门就该报问题
	normalDoc := "# 项目说明\n\n本文件介绍怎么用。\n" // 普通文档：不该被内容门禁误伤
	cases := []struct {
		name           string
		path           string
		body           string
		wantArtifact   bool
		wantClaims     bool
		wantProblemsGt int
	}{
		{"标准形态+自称报告正文 ⇒ 必须报问题", "docs/五轴审查-x.md", placeholder, true, true, 0},
		{"标准形态+普通文档 ⇒ 不进门不报（T66 误伤教训）", "docs/readme.md", normalDoc, true, false, 0},
		{"带 ./ 前缀 ⇒ 路径侧被跳过（绕过面，钉成可见事实）", "./docs/五轴审查-x.md", placeholder, false, true, 0},
		{"子目录下的 docs/ ⇒ 同上跳过", "sub/docs/五轴审查-x.md", placeholder, false, true, 0},
		{"绝对路径 ⇒ 同上跳过", "D:/wb/docs/五轴审查-x.md", placeholder, false, true, 0},
		// 事实边界（本轮实测纠正我自己的想象）：根目录 .md 只有在**自称报告**时才被 T74 拦；
		// 而 claimsReport 认的是「正文行首有三节标题」或「文件名含 报告/审查/review」——
		// 一份落在根目录、文件名不含这些词、正文也没写三节的占位 .md，**两道门都不管**（不是 bug 复现，
		// 是当前口径的真实边界）。台账里那 3 份真报告全部走标准 docs/ 路径 ⇒ 无绕过证据，登记不扩大覆盖。
		{"根目录+文件名不含报告词+无三节 ⇒ 两门都不触发（当前口径边界）", "a.md", placeholder, false, false, 0},
		{"根目录但文件名含报告词 ⇒ claims 为真、由 T74 落点门禁接手", "a-审查.md", placeholder, false, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotArtifact := isReportArtifact(tc.path)
			gotClaims := claimsReport(tc.path, tc.body)
			probs := len(reportGateProblems(tc.path, []byte(tc.body)))
			if gotArtifact != tc.wantArtifact {
				t.Errorf("isReportArtifact(%q)=%v，期望 %v（口径若变了，本表与注释需同步更新）", tc.path, gotArtifact, tc.wantArtifact)
			}
			if gotClaims != tc.wantClaims {
				t.Errorf("claimsReport(%q)=%v，期望 %v", tc.path, gotClaims, tc.wantClaims)
			}
			fired := gotArtifact && gotClaims
			if fired && probs <= tc.wantProblemsGt {
				t.Errorf("%s：两个谓词都为真却报 %d 个问题 ⇒ 不合格正文被放行", tc.path, probs)
			}
			if !fired && probs > 0 {
				t.Errorf("%s：未真正生效（artifact=%v claims=%v）却报 %d 个问题 ⇒ 两谓词与门禁实现口径分叉",
					tc.path, gotArtifact, gotClaims, probs)
			}
			// 跳过的那几种形态必须报 0 —— 这正是"零问题不等于合格"的根据，写进日志备查
			t.Logf("artifact=%-5v claims=%-5v 生效=%-5v 问题数=%d ｜ %s", gotArtifact, gotClaims, fired, probs, tc.path)
		})
	}
}
