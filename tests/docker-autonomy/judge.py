import json, os, sys

D = "/tmp/workbench-output"
ok = True

def chk(name, cond, detail=""):
    global ok
    print(("  [PASS] " if cond else "  [FAIL] ") + name + ((" " + detail) if (not cond and detail) else ""))
    if not cond:
        ok = False

gpath = os.path.join(D, "autonomy", "goals.json")
dpath = os.path.join(D, "autonomy", "decisions.jsonl")
jpath = os.path.join(D, "logs", "workbench.jsonl")

if not os.path.exists(gpath):
    chk("2 目标 bootstrap（goals.json 存在）", False, gpath)
    print("SUMMARY_FAIL"); sys.exit(1)

goals = json.load(open(gpath))
dec = [json.loads(l) for l in open(dpath) if l.strip()] if os.path.exists(dpath) else []
kinds = [d.get("kind") for d in dec]
titles = [g.get("title") for g in goals.get("goals", [])]

chk("2 目标 bootstrap（active + 机器判据）",
    any(g.get("status") == "active" and g.get("criteria") for g in goals.get("goals", [])), str(titles))
chk("3 目标推导阶段任务（plan 决策 >= 1）", kinds.count("plan") >= 1, "plan=%d" % kinds.count("plan"))

tdir = os.path.join(D, "tasks")
tasks = []
if os.path.isdir(tdir):
    for name in os.listdir(tdir):
        tasks.append(name)
withres = [t for t in tasks if os.path.exists(os.path.join(tdir, t, "workspace", "result.py"))]
chk("4 产物真实落盘（workspace/result.py）", len(withres) >= 1, "tasks=%d with_result=%d" % (len(tasks), len(withres)))

ver = []
for t in tasks:
    p = os.path.join(tdir, t, "verify.json")
    if os.path.exists(p):
        try:
            ver.append(json.load(open(p)))
        except Exception:
            pass
chk("5 真实执行验收通过（verify.json pass）", any(v.get("pass") for v in ver), "verify_files=%d" % len(ver))

stages = 0
for g in goals.get("goals", []):
    stages += len(g.get("stages") or [])
chk("6 自主验收记账（accept >= 1）", kinds.count("accept") >= 1, "accept=%d" % kinds.count("accept"))
chk("7 阶段推进（stages >= 2）", stages >= 2, "stages=%d" % stages)

chk("8a 目标完成（goalDone >= 1）", kinds.count("goalDone") >= 1, "goalDone=%d" % kinds.count("goalDone"))
chk("8b 自动下一目标（nextGoal >= 1）", kinds.count("nextGoal") >= 1, "nextGoal=%d" % kinds.count("nextGoal"))
comp = [g for g in goals.get("goals", []) if g.get("status") == "completed"]
aid = goals.get("activeId")
act = [g for g in goals.get("goals", []) if g.get("id") == aid]
chk("8c completed 旧目标 + activeId 指向新目标",
    len(comp) >= 1 and len(act) == 1 and act[0].get("id") != comp[0].get("id"),
    "completed=%d activeId=%s" % (len(comp), aid))

# 判据 9（固定模板兼容）由 --acceptance 的 A3 项单独核对，本脚本不重复判定，
# 避免出现"永远为真"的伪判据（零数据不记 PASS）。

print("SUMMARY_PASS" if ok else "SUMMARY_FAIL")
sys.exit(0 if ok else 1)
