package modelpool

// egressFailureFingerprints 是**出口级**失败的正文指纹（一律小写，与 ToLower(detail) 比对）。
//
// 取值纪律（照 bootAttribution.go 的教训写窄）：只认「上游写明整条出口不可用」的形态。
// 任何可能只是瞬时抖动的措辞都不许进来 —— 把车道级误判成出口级会**停止换模型**并按整池处置，
// 代价是把 21~33 条健康车道一起熔掉；反过来漏判只是多试几条车道（每个约 3 秒）。
// 两边代价不对称 ⇒ 宁可漏判。
var egressFailureFingerprints = []string{
	// Node 的自签证书链：实测这两条总是成对出现，且与"他车道同窗成功"共存率 = 0.0% ⇒ 出口级最强一档。
	"self signed certificate",
	"depth_zero_self_signed_cert",
}
