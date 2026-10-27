package engine

// effectiveMinWords 按「分档降载」给出本轮的产出体量参考（round 41 · T17③）。
//
// 为什么（40 轮实测的硬事实）：免费车道在「一次要交约 1000 字 + 多个文件 + 验收命令」的要求下，
// 几乎必然零产出或不合规 —— 从没有任何一次合规产物真正落地到工作区。
// 分档的取舍：**首轮先要小而能验收的东西**（1 个文件 + 1 条 verify），把「交得出来」的概率抬起来；
// 后续轮次（修复/追加）再回到模板声明的体量，避免一上来就贪大。
//
// 200 的下限是刻意取的：与产出物协议里「散文过短」的既有门槛同量级，
// 既能让首轮的交付物小到做得完，又不会让「只写一句话」被当成合规产出。
func effectiveMinWords(base int, attempt int) int {
	if base <= 0 {
		base = 300 // 与 minWordsOf 的无模板兜底一致
	}
	if attempt > 0 {
		return base
	}
	if base > 200 {
		return 200
	}
	return base
}
