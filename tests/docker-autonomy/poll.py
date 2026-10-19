import json,re,os,urllib.request,sys
def get(url,token):
    r=urllib.request.Request(url); r.add_header("X-WB-Token",token)
    with urllib.request.urlopen(r,timeout=20) as resp: return resp.read().decode("utf-8","replace")
url=open("/tmp/runtime/panel.url").read().strip()
token=open("/tmp/token").read().strip()
try:
    a=json.loads(get(url+"api/autonomy?decisions=60",token))
except Exception as e:
    print("API_ERR",e); sys.exit(0)
dec=a.get("decisions") or []
kinds={}
for d in dec: kinds[d.get("kind","?")]=kinds.get(d.get("kind","?"),0)+1
goals=a.get("goals") or []
done=[g for g in goals if g.get("status")=="completed"]
print("kinds="+",".join(f"{k}={v}" for k,v in sorted(kinds.items())))
print("goals=%d completed=%d active=%s stages=%s progress=%s enabled=%s" % (
    len(goals), len(done), (a.get("active") or {}).get("title"), a.get("stage"), a.get("progress"), a.get("enabled")))
if os.path.exists("/tmp/workbench-output/autonomy/decisions.jsonl"):
    txt=open("/tmp/workbench-output/autonomy/decisions.jsonl").read()
    print("raw_goalDone=%d raw_nextGoal=%d raw_accept=%d" % (
        txt.count('"kind":"goalDone"'), txt.count('"kind":"nextGoal"'), txt.count('"kind":"accept"')))
