package gov

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestChecklistStatesAreSelfConsistent 钉住「考核校验三态」与计数的自洽性。
//
// 一手代价（2026-10-21 前端走查实测）：CheckItem 只有 bool Pass，unverified 被
// 序列化成 "pass":false，前端又去读根本不存在的 it.level ⇒ 六格全渲染 FAIL，
// 而头部写着「3 PASS / 2 FAIL / 1 未验证」——同一页两处读数互相矛盾。
// 本测试从两个方向堵住：① 每项必须有合法 State；② State 分布必须等于 Pass/Fail/Unverified。
func TestChecklistStatesAreSelfConsistent(t *testing.T) {
	var sum Summary
	sum.Contracts.Total = 4
	sum.Contracts.ComplianceAvg = 88
	sum.Volume.Samples = 5
	ck := BuildChecklist(sum)
	if len(ck.Items) == 0 {
		t.Fatal("BuildChecklist 未产出任何考核项")
	}
	var okN, failN, unkN int
	for _, it := range ck.Items {
		switch it.State {
		case "ok":
			okN++
			if !it.Pass {
				t.Errorf("%s State=ok 但 Pass=false（两字段漂移）", it.Code)
			}
		case "fail":
			failN++
			if it.Pass {
				t.Errorf("%s State=fail 但 Pass=true（两字段漂移）", it.Code)
			}
		case "unverified":
			unkN++
		default:
			t.Fatalf("%s 的 State=%q 非法（必须是 ok/fail/unverified）", it.Code, it.State)
		}
	}
	if okN != ck.Pass || failN != ck.Fail || unkN != ck.Unverified {
		t.Fatalf("State 分布 {ok:%d fail:%d unk:%d} 与计数 {pass:%d fail:%d unverified:%d} 不符",
			okN, failN, unkN, ck.Pass, ck.Fail, ck.Unverified)
	}
	if okN+failN+unkN != len(ck.Items) {
		t.Fatalf("三态项数 %d ≠ 总项数 %d（有项未落态）", okN+failN+unkN, len(ck.Items))
	}
}

// TestChecklistUnverifiedIsNotSerializedAsFail 钉住序列化面：unverified 项必须在 JSON 里
// 显式带 state=unverified，否则前端只能看到 pass:false 并把它读成 FAIL。
func TestChecklistUnverifiedIsNotSerializedAsFail(t *testing.T) {
	var sum Summary // 全零：契约 0 份、采样 0 点 ⇒ 必有 unverified 项
	ck := BuildChecklist(sum)
	if ck.Unverified == 0 {
		t.Skip("当前零数据形态未产生 unverified 项（K4 分支变化时本用例失去意义）")
	}
	b, err := json.Marshal(ck)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"state":"unverified"`) {
		t.Fatalf("序列化结果缺少 state=unverified，前端会把「取不到数」渲染成 FAIL：%s", string(b))
	}
}
