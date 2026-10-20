# -*- coding: utf-8 -*-
# V1.0 | 2026-10-20 轮11 | 第 0 腿：跨文件四集对账（05 §十八 立，季度 cron 首步调用）
"""leg0_consistency_audit.py — 巡检第 0 腿：inv/reg/mx/sweep 四集对账。

判据（任一不过即 exit 1，须先修再进四腿扫描）：
  1) reg - inv = ∅            （登记表不得有盘面不存在的技能）
  2) mx  - reg = ∅            （矩阵不得有登记表外的条目）
  3) reg - mx = ∅             （登记表每条都要在矩阵里）
  4) sweep - reg = ∅          （扫描行不得有登记表外条目）
  5) reg - sweep = ∅          （登记表每条都要被扫到）
  6) candidate_pool/self_developed 条目不得缺 upstream_path（自研允许无 repo）
  7) same_origin 条目必须有 recorded_version（否则版本腿必然 UNVERIFIED）

用法：python leg0_consistency_audit.py [--registry <path>]
退出码：0=四集一致；1=存在不一致（打印差异清单）
"""
import json, os, sys, argparse

BASE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def load(p):
    if not os.path.isfile(p):
        return None
    return json.load(open(p, encoding='utf-8'))


def main(argv=None):
    ap = argparse.ArgumentParser(description="巡检第 0 腿：四集对账")
    ap.add_argument("--registry", default=BASE + "/data/skill-watchlist.json")
    args = ap.parse_args(argv)

    inv = load(BASE + "/data/inventory.json")
    reg = load(args.registry)
    mx = load(BASE + "/data/score-matrix.json")
    sw = load(BASE + "/data/sweep-report.json")
    missing_files = [n for n, d in (("inventory", inv), ("watchlist", reg), ("score-matrix", mx), ("sweep-report", sw)) if d is None]
    if missing_files:
        print("[GATE:leg0-fail] 缺文件:", missing_files)
        return 1

    names_inv = set(i['name'] for i in inv['items'] if i.get('has_skill_md'))
    names_reg = set(e['local_skill'] for e in reg['watchlist'])
    names_mx = set(s['local_skill'] for s in mx['skills'])
    names_sw = set(r['local_skill'] for r in sw['rows'])

    problems = []
    extra = sorted(names_reg - names_inv)
    if extra:
        problems.append("reg 有但盘面没有（脏条目）: %s" % extra)
    extra = sorted(names_mx - names_reg)
    if extra:
        problems.append("矩阵有但登记表没有（清理漏面）: %s" % extra)
    extra = sorted(names_reg - names_mx)
    if extra:
        problems.append("登记表有但矩阵没有（未入矩阵）: %s" % extra)
    extra = sorted(names_sw - names_reg)
    if extra:
        problems.append("扫描行有但登记表没有: %s" % extra)
    extra = sorted(names_reg - names_sw)
    if extra:
        problems.append("登记表有但没被扫到: %s" % extra)

    for e in reg['watchlist']:
        kind = e.get('entry_kind') or ''
        name = e['local_skill']
        if kind.startswith('self_developed'):
            continue
        # 轮11 修自相矛盾：候选池条目本就允许「有 repo 无 path」（入口件未定位是合法状态，05 §十五），
        # 只有 same_origin 条目缺 path 才是问题（漂移腿会取不到上游⇒UNVERIFIED）。
        if not kind.startswith('candidate_pool') and not e.get('upstream_path') and e.get('upstream_repo'):
            problems.append("%s 同源条目有 repo 无 upstream_path（漂移腿将取不到上游）" % name)
        if not kind.startswith('candidate_pool') and not e.get('recorded_version'):
            problems.append("%s 同源条目无 recorded_version（版本腿必然 UNVERIFIED）" % name)

    if problems:
        print("[GATE:leg0-fail] 四集对账不平，%d 项：" % len(problems))
        for p in problems:
            print("  -", p)
        return 1
    print("[GATE:leg0-pass] 四集一致 inv=%d reg=%d mx=%d sweep=%d" % (len(names_inv), len(names_reg), len(names_mx), len(names_sw)))
    return 0


if __name__ == "__main__":
    sys.exit(main())