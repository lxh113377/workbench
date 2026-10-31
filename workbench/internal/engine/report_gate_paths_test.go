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
// 为什么要钉这张表：探针实测把同一份不合格正文以 `./docs/报告.md` **直接**喂给谓词时报 0 个问题
// （isReportArtifact 按前缀匹配）。第一版探针我正是踩了这个形态，把三份现场产物全读成"判合格"
// —— 空集读成 PASS，本会话登记过的老毛病第 4 次。⇒ 断言强制："进门"与"合格"必须是两件事。
//
// ⚠️ 本表只描述**谓词层**行为，不等于**生产链路**可绕过（round 155 独立复核 + Lead 亲跑纠正了我
// 原先写在这里的错误定性，原文见 git 历史；教训：判据注释不能把"直接调谓词"的结果当成生产事实）：
//
//	· `./docs/x.md`   —— artifact.SafePath 会 TrimPrefix("./") 归一成 `docs/x.md` ⇒ **照样进门报问题**，
//	  不是绕过面。我上一轮把它记成"路径侧被跳过"是把谓词事实当成了链路事实。
//	· 绝对路径        —— SafePath 直接硬拒（"必须是相对路径"）⇒ 交付根本进不来，属**已有防御**，
//	  我上一轮把它记成"缺口"，等于把防御记成漏洞。
//	· 子目录 sub/docs —— 确实进门禁射程之外，但那是**落点口径**（目标指定 docs/），非绕过。
//	· 单反斜杠 docs\x.md —— **这才是真绕过面**（SafePath 只归一双反斜杠）：内容门跳过、落点门
//	  的 isDocsMarkdown 却做反斜杠归一并 return nil ⇒ 两道门同时失效。已修 + 判据见
//	  backslash_severity_test.go（同表另记：a.md 现场正文 claims=false，我上一轮"已被 T74 拦下"
//	  的说法当场可证伪，已在 E37 撤回）。
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
		// 三行的 wantArtifact=false 是**谓词层**事实；生产链路会先过 SafePath，故均非绕过面（见函数头注释）。
		{"谓词层：./ 前缀不匹配（生产链路由 SafePath 归一 ⇒ 非绕过）", "./docs/五轴审查-x.md", placeholder, false, true, 0},
		{"谓词层：子目录 docs/ 不匹配（属落点口径，非绕过）", "sub/docs/五轴审查-x.md", placeholder, false, true, 0},
		{"谓词层：绝对路径不匹配（SafePath 硬拒，属已有防御）", "D:/wb/docs/五轴审查-x.md", placeholder, false, true, 0},
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
