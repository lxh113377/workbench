// plugin_contract_harness.mjs -- 用**真实 OFM 插件代码**校验池子投影出的 aggregate 配置。
//
// 为什么存在：executor.taskHome 的 aggregate 接线尚未做（Lead 在途文件），
// 而「池里的自填端点能否被 DSH 真调用」取决于插件是否接受我们的 JSON 形状。
// 插件才是消费方 —— 它说行才算数。字段名或枚举值在插件侧一旦改名，
// 本 harness 立刻产不出行，契约测试翻红；而不是等接线上线后表现为
// 「静默改道免费车道」（用户以为在用自建端点，实际在烧免费额度）。
//
// 用法: node plugin_contract_harness.mjs <aggregate.json> <pluginDir>
// 输出: 单行 JSON { rows:[{id,channel,providerId,modelId,format,wire}], chatUrl, error }
// 退出码: 0 = 插件成功解析（哪怕 rows 为空，由调用方判定）；非 0 = 环境/导入问题
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import path from "node:path";

const [cfgPath, pluginDir] = process.argv.slice(2);
if (!cfgPath || !pluginDir) {
  console.error("用法: node plugin_contract_harness.mjs <aggregate.json> <pluginDir>");
  process.exit(2);
}

const modPath = path.join(pluginDir, "src", "aggregate.js");
let m;
try {
  m = await import(pathToFileURL(modPath).href);
} catch (e) {
  console.error("导入插件失败: " + modPath + " :: " + e.message);
  process.exit(3);
}
if (typeof m.buildAggregateCatalog !== "function") {
  console.error("插件未导出 buildAggregateCatalog（契约已变更）");
  process.exit(4);
}

let cfg;
try {
  let text = readFileSync(cfgPath, "utf8");
  // 剥 UTF-8 BOM：Windows PowerShell 5.1 的 Set-Content -Encoding UTF8 会写 BOM，
  // 而 JSON.parse 遇到 \uFEFF 直接报 Unexpected token。产品路径上 settings.json 由
  // Go 的 WriteJSONAtomic 写出（无 BOM），但测试夹具与人工排查常用 PS 生成，
  // 消费方宽容比要求每个产出方都记得去 BOM 更实际。
  if (text.charCodeAt(0) === 0xfeff) text = text.slice(1);
  cfg = JSON.parse(text);
} catch (e) {
  console.error("读取 aggregate 配置失败: " + e.message);
  process.exit(5);
}

const out = { rows: [], chatUrl: "", error: "" };
try {
  const rows = m.buildAggregateCatalog(cfg) || [];
  out.rows = rows.map((r) => ({
    id: r.id, channel: r.channel, providerId: r.providerId,
    modelId: r.modelId, format: r.format, wire: r.wire,
  }));
  if (rows.length > 0 && typeof m.resolveProvider === "function") {
    const rp = m.resolveProvider(cfg, rows[0].providerId);
    out.chatUrl = rp.chatUrl || rp.url || "";
  }
} catch (e) {
  out.error = String(e && e.message ? e.message : e);
}
process.stdout.write(JSON.stringify(out));
