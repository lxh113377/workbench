import json, subprocess, datetime
def git_show(rev, path):
    out = subprocess.run(['git','-C',r'D:/workdesktop','show',rev+':'+path], capture_output=True)
    return json.loads(out.stdout.decode('utf-8','replace'))
cur = json.load(open(r'D:/workdesktop/docs/skill-audit/data/inventory.json', encoding='utf-8'))
old = git_show('1d54832', 'docs/skill-audit/data/inventory.json')
def top1set(d): return {i['name']: (i.get('route_top1_count') or 0) for i in d['items'] if i.get('has_skill_md')}
o, n = top1set(old), top1set(cur)
skills = sorted(set(o) | set(n))
rows = [{'skill': s, 'pre': o.get(s), 'post': n.get(s), 'cum': (o.get(s) or 0)+(n.get(s) or 0)} for s in skills]
rows.sort(key=lambda r: -r['cum'])
zero = [r['skill'] for r in rows if r['cum']==0]
led = {'schema':'top1-cumulative-ledger-v1','built_at':datetime.datetime.now().isoformat(timespec='seconds'),
       'windows':{'pre':'inventory@1d54832 (trace 10.06MB 轮转前窗口)','post':'inventory 2026-10-20 (trace 轮转后 168KB)'},
       'cum_zero_count':len(zero),'cum_covered':len(rows)-len(zero),'rows':rows}
json.dump(led, open(r'D:/workdesktop/docs/skill-audit/data/top1-cumulative-ledger.json','w',encoding='utf-8'), ensure_ascii=False, indent=2)
md = ['# 路由 top1 跨轮转累计台账（B6 观测基础）','', '> 生成 '+led['built_at']+'；口径 = git 冻结版 inventory(轮转前窗口) ∪ 当前 inventory(轮转后窗口)，两窗相加。','', '- 累计有命中技能：**%d / %d**' % (led['cum_covered'], len(rows)), '- 两窗累计仍 0（B6 候选池，须第三窗确认才进退役流程）：**%d**' % led['cum_zero_count'], '', '| 技能 | 轮转前 | 轮转后 | 累计 |', '|---|---|---|---|']
for r in rows:
    md.append('| %s | %s | %s | %d |' % (r['skill'], r['pre'] if r['pre'] is not None else '-', r['post'] if r['post'] is not None else '-', r['cum']))
open(r'D:/workdesktop/docs/skill-audit/07-top1累计台账.md','w',encoding='utf-8').write(chr(10).join(md)+chr(10))
print('ledger built: covered=%d zero=%d rows=%d' % (led['cum_covered'], led['cum_zero_count'], len(rows)))