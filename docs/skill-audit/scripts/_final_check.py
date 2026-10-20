import json, os, glob
BASE = "D:/workdesktop/docs/skill-audit"
docs = ["00-总览.md","01-现状盘点.md","02-问题分析报告.md","03-对比评估矩阵.md","04-迁移替换计划.md","05-技能巡检与维护规范.md","05-季度巡检报告.md","06-验收报告.md"]
bad = []
for d in docs:
    p = os.path.join(BASE, d).replace(chr(92), "/")
    ok = os.path.isfile(p)
    sz = os.path.getsize(p) if ok else 0
    A = "" if False else None
    print("%-38s %s %8d" % (d, "OK" if ok else "MISSING", sz))
    if not ok: bad.append(d)
print("--- JSON validity ---")
for f in sorted(glob.glob(BASE + "/data/*.json")) + sorted(glob.glob(BASE + "/raw/*.json")):
    try:
        json.load(open(f, encoding="utf-8")); print("  OK  ", os.path.basename(f), os.path.getsize(f))
    except Exception as e:
        print("  BAD ", os.path.basename(f), repr(e)); bad.append(os.path.basename(f))
print("--- scripts syntax ---")
import ast
for f in sorted(glob.glob(BASE + "/scripts/*.py")):
    try:
        ast.parse(open(f, encoding="utf-8").read()); print("  OK  ", os.path.basename(f))
    except Exception as e:
        print("  BAD ", os.path.basename(f), repr(e)); bad.append(os.path.basename(f))
print("BAD =", bad)