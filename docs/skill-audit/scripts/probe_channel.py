# -*- coding: utf-8 -*-
# V1.0 | 2026-10-20 轮13 | 限流探测件（cron 第 0.5 步的可执行化）
"""probe_channel.py - ungh.cc 通道限流探测（巡检四腿前置门）。

判据（05 §十八 补丁二立）：探测 N 个仓（默认 8，含各主力上游），
失败（429/403/timeout）>= 3 => 判「限流期」。
限流期处置：只跑不依赖网络的腿（leg0/盘点），四腿扫描延后；
          若强跑四腿，sweep-report.json 必须记 rate_limit_pollution。

退出码：0=通道可用（可跑四腿）；3=限流期（勿跑四腿）。
"""
import json, sys, time, urllib.request, argparse

REPOS = [
    "obra/superpowers",
    "addyosmani/agent-skills",
    "vercel-labs/agent-browser",
    "kepano/obsidian-skills",
    "wshobson/agents",
    "TencentCloudBase/CloudBase-AI-Toolkit",
    "zarazhangrui/frontend-slides",
    "komako-workshop/digital-oracle",
]
FAIL_THRESHOLD = 3


def probe(repo, timeout=15):
    """-> (ok: bool, detail: str)。"""
    try:
        req = urllib.request.Request("https://ungh.cc/repos/" + repo, headers={"User-Agent": "Mozilla/5.0"})
        raw = urllib.request.urlopen(req, timeout=timeout).read().decode("utf-8", "replace")
        d = json.loads(raw)
        r = d.get("repo") or {}
        return True, "stars=%s" % r.get("stars")
    except Exception as e:
        return False, repr(e)[:60]


def main(argv=None):
    ap = argparse.ArgumentParser(description="ungh.cc 限流探测")
    ap.add_argument("--repos", default=",".join(REPOS))
    ap.add_argument("--sleep", type=float, default=1.2)
    args = ap.parse_args(argv)
    repos = [r.strip() for r in args.repos.split(",") if r.strip()]
    ok = 0
    fails = []
    for rp in repos:
        good, detail = probe(rp)
        if good:
            ok += 1
            print("OK   %-38s %s" % (rp, detail))
        else:
            fails.append((rp, detail))
            print("FAIL %-38s %s" % (rp, detail))
        time.sleep(args.sleep)
    limited = len(fails) >= FAIL_THRESHOLD
    verdict = "LIMITED(限流期，勿跑四腿)" if limited else "AVAILABLE(通道可用)"
    print("[PROBE] ok=%d/%d fails=%d threshold=%d => %s" % (ok, len(repos), len(fails), FAIL_THRESHOLD, verdict))
    if limited:
        print("[PROBE] 处置=只跑 leg0+盘点，四腿延后，强跑须记 rate_limit_pollution")
    return 3 if limited else 0


if __name__ == "__main__":
    sys.exit(main())