import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/sweep-report.json', encoding='utf-8'))
rows = {r['local_skill']: r for r in d['rows']}
V = {}
for n in ('discover-agent-cli','local-computer-use','figma','spike'):
    r = rows.get(n)
    if r and r['leg_version']['state'] == 'RED':
        V[n] = r['leg_version']['note']
n = 0
for e in reg['watchlist']:
    name = e['local_skill']
    if name in V:
        # 从 note 里解析上游观测版本
        import re
        m = re.search(r'上游观测 ([^（(]+)', V[name])
        live = m.group(1).strip() if m else None
        old = e.get('recorded_version')
        if live:
            e['recorded_version'] = live
            e['last_checked'] = '2026-10-20'
            e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 轮9 裁决=更新候选基线（%s → %s）：候选池条目版本腿 RED=候选仓发新版，证明候选活跃维护中；非吸收（候选非同源，仅刷新对表基线）' % (old, live)
            n += 1
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('baselines updated:', n)
for e in reg['watchlist']:
    if e['local_skill'] in V: print('   %-26s -> %s' % (e['local_skill'], e.get('recorded_version')))