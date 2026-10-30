package model

// DeliverableGateMarker 是「交付物门禁」条款的标题前缀，由引擎自己注入到任务 prompt 里。
//
// 为什么要有这个常量（round 135 · T88）：引擎侧要知道「这个任务是不是被要求交一份报告文件」，
// 而唯一可靠的依据就是它当初写进 prompt 的那句话。此前这句话只以字面量存在于两个包里
// （templates.deliverableGateClause 与 autonomy.autonomyDeliverableGate，标题措辞还略有不同），
// 引擎若自己再抄一份就成了第三份私有清单 —— 改了注入侧忘了判定侧就会静默失效
// （本会话同族病：T57/T61/T72/T80/T84/T86 全是"同一契约多处实现"）。
// ⇒ 单点放这里（templates 与 autonomy 都只依赖 model，不造成任何循环）。
//
// 取值是两侧标题的**共同前缀**："## 交付物门禁"，因此注入侧无论怎么补后缀，判定侧都认得。
// 注意：不要用任务标题的词族（报告/审查/体检…）当判据 —— 实测那是 1,120:5 的口径
// （命中面巨大而真缺陷只有 5 条），会把没打算交报告的模板任务一起判红。
const DeliverableGateMarker = "## 交付物门禁"
