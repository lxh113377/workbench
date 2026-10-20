import json
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
ent = {e['local_skill']: e for e in reg['watchlist']}
names = ['test-driven-development','spike','first-principles-decomposer','A-dynamic-workflow','A-skill-manager','html-ppt','A-project-handoff','computer-use-guidance-windows','A-memory-start','audit-runner-safe-aggregate','brand-guidelines','chart-visualization','data-visualization','frontend-slides','oracle','story','video-frames','wechat-automation']
for n in names:
    r = rows.get(n); e = ent.get(n)
    if not r: continue
    repo = (e or {}).get('upstream_repo') or 'NONE'
    print('%-30s %-38s %s' % (n, repo[:38], r['leg_drift']['note'][:78]))