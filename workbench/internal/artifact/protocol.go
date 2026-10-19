package artifact

// ProtocolInstruction 是注入到每个任务 prompt 尾部的输出协议说明。
//
// 设计目标：模型一步就能照做；即使不照做也能被检出（解析器三级降级 +
// 结构缺陷计入返工），而不是像改造前那样默默接受一段作文。
//
// 关键教训（2026-10-19 现场，两层转义事故）：
//  1) 第一版把围栏写成 Go 转义序列的双重转义形式，模型读到字面量乱码；
//  2) 第二版只拼了单个反引号，围栏变成 "`" 而不是 "```"，同样不可用。
// 本文件用 FENCE 常量（三个反引号）拼接，并有专门的契约测试校验渲染结果。
const ProtocolInstruction = "" +
	"\n## 产出协议（必须遵守，工作台会据此落盘并真实验收）\n" +
	"你的回答除了说明文字，必须包含一段产物块。工作台会把其中的文件写入磁盘，\n" +
	"并真实执行 verify 里的命令；命令不通过会带着报错让你返工。\n\n" +
	"格式（下例照抄，围栏就是三个反引号）：\n\n" +
	"<<<ARTIFACTS>>>\n" +
	FENCE + "manifest\n" +
	`{"files":[{"path":"tool.py","lang":"python"}],` + "\n" +
	` "verify":[{"cmd":"python tool.py --help","expectExit":0,"timeout":60}]}` + "\n" +
	FENCE + "\n" +
	FENCE + "file:tool.py\n" +
	"（该文件的完整内容，必须以闭合围栏结束，不允许【此处省略】）\n" +
	FENCE + "\n" +
	"<<<END>>>\n\n" +
	"硬性要求：\n" +
	"1. 文件内容必须完整可运行；围栏必须闭合；禁止出现【省略】【其余同上】【见上文】。\n" +
	"2. 路径只能是相对路径（如 tool.py、docs/a.md），禁止 .. 与盘符。\n" +
	"3. verify 至少 1 条能在本机真实执行的命令（解释器限 python/node/pwsh/go），\n" +
	"   expectExit 写期望退出码（正常退出为 0）。\n" +
	"4. 若不产出代码，也要产出至少一个 .md 成果文件，并用\n" +
	"   pwsh -NoProfile -Command 【if (Test-Path a.md) { exit 0 } else { exit 1 }】\n" +
	"   这类命令证明文件真的存在。\n" +
	"5. 单个文件不超过 2MB，文件数不超过 20。\n"

// FENCE 是三个反引号，即 Markdown 代码围栏。
// 用常量拼接而不是字面量：Go raw string 会被三个反引号截断；
// 用转义序列又容易写成双重转义（本项目已踩过两次）。
const FENCE = "```"