package modelpool

import (
	"context"
	"errors"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// egressFailingRunner 只为本测试服务：首条车道（"a"）按指定 Kind + 指定上游原文失败，
// 其余车道成功 —— 于是「有没有继续换模型」直接读 calls 长度即可。
// judge() 在 err != nil 时把 err.Error() 当作 detail，所以正文判据走的就是这条字符串。
type egressFailingRunner struct {
	calls   []string
	kind    string
	message string
}

func (r *egressFailingRunner) UpdateConfig(config.ExecutorConfig) {}

func (r *egressFailingRunner) Run(_ context.Context, _ *model.Task, modelID string) (*executor.Result, error) {
	r.calls = append(r.calls, modelID)
	if modelID == "a" {
		return &executor.Result{ErrorKind: r.kind}, errors.New(r.message)
	}
	return &executor.Result{FinalText: "产出正常"}, nil
}

// runEgressSwitchCase 跑一次「首条车道失败」的故障转移，把 (调用序列, 结果, 错误) 原样交回。
//
// 断言不在这里做 —— 两档的**正确结果本来就不一样**（我第一版把共同断言放这儿，于是反向半边全红）：
//
//	· 出口级：停止换模型 ⇒ calls 只有 1，错误原样上抛给引擎去做池级处置；
//	· 车道级：继续换模型 ⇒ 下一条车道成功 ⇒ calls=2、err=nil、有产出 ——
//	  这正是既有 TestFailoverSwitchesWithoutUserVisibleFailure 钉过的行为（非限流失败不该让用户看见错误）。
//
// ⇒ 判据必须按各自口径断言，否则"共同的形状"会假红/假绿两边。
func runEgressSwitchCase(t *testing.T, kind, message string) (*egressFailingRunner, *executor.Result, error) {
	t.Helper()
	m := NewManager(NewStore(t.TempDir()), []Spec{
		spec("a", 10, 100, StateUnknown),
		spec("b", 20, 50, StateUnknown),
	})
	inner := &egressFailingRunner{kind: kind, message: message}
	fr := NewFailoverRunner(inner, m, NewScheduler(StrategyPriority))
	res, err := fr.Run(context.Background(), &model.Task{ID: "t-egress"}, "a")
	if res == nil {
		t.Fatal("必须返回结果对象（供引擎读 ErrorKind/StderrPath），实得 nil")
	}
	return inner, res, err
}

// TestFailoverStopsOnEgressScopedNetworkFailure 钉住 T86 第一档的行为：
// 上游正文明确是出口级（证书链）时，第一条车道失败后就**不再换模型**。
// 取证依据（两条独立通路一致）：自签证书类与「他车道同窗成功」共存率 = 0/1799 = 0.0%
// ⇒ 整条出口都不通；而改造前会继续换模型，实测一次任务把池里 32 条车道轮了一遍（每条约 3 秒白试）。
func TestFailoverStopsOnEgressScopedNetworkFailure(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		message string
	}{
		{"transport_failed + 证书链全文", string(executor.KindTransport), "dsh: TRANSPORT: model request failed: fetch failed - self signed certificate; try running Node.js with --use-system-ca (DEPTH_ZERO_SELF_SIGNED_CERT)"},
		{"stream_cut + 证书链全文（同一指纹判别，不因 Kind 例外）", string(executor.KindStreamCut), "TRANSPORT: self signed certificate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner, res, err := runEgressSwitchCase(t, tc.kind, tc.message)
			if len(inner.calls) != 1 {
				t.Fatalf("出口级失败必须停止换模型，实际调用 %v（%d 次）⇒ 会白试整池", inner.calls, len(inner.calls))
			}
			// 停止换模型后必须把原始失败交回引擎：引擎据此做池级冷却/长等待预算（E10）
			if err == nil || res.ErrorKind != tc.kind {
				t.Fatalf("出口级失败必须原样上抛，实得 err=%v kind=%q", err, res.ErrorKind)
			}
		})
	}
}

// TestFailoverKeepsSwitchingOnLaneScopedNetworkFailure 是反向半边 —— 这条改动唯一的价值所在：
// 同为 transport_failed，但正文没有出口级指纹（对端超时）时**必须继续换模型**；
// stream_cut 无指纹同理（实测 45.1% 与他车道成功共存 ⇒ 多数真是车道级）；
// 不在表内 scope 名单的 Kind（如 bad_exit）即使正文带证书字样也不升档。
// 少了这一半，"加个停止条件"就是拿 21~33 条健康车道陪葬。
func TestFailoverKeepsSwitchingOnLaneScopedNetworkFailure(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		message string
	}{
		{"transport_failed + 对端超时 ⇒ 继续换模型", string(executor.KindTransport), "dsh: TRANSPORT: fetch failed: ETIMEDOUT 10.0.0.1:443"},
		{"stream_cut 普通流断 ⇒ 继续换模型", string(executor.KindStreamCut), "our free model closed the stream before its finish token, without answering"},
		{"bad_exit 带证书字样也不升档（Kind 不在 scope 名单）", string(executor.KindExit), "self signed certificate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner, res, err := runEgressSwitchCase(t, tc.kind, tc.message)
			if len(inner.calls) != 2 {
				t.Fatalf("车道级失败必须继续换模型（换一条正是正确处置），实际调用 %v", inner.calls)
			}
			// 下一条车道成功 ⇒ 不该把失败暴露给用户（与既有 TestFailoverSwitchesWithoutUserVisibleFailure 同口径）
			if err != nil || res.FinalText == "" {
				t.Fatalf("切换到健康车道后应正常交付，实得 err=%v res=%+v", err, res)
			}
		})
	}
}
