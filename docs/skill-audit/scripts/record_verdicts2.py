import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'code-review':
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 裁决=吸收 依据=字节比0.236/上游独有36标题→吸收24节至1.045(独有0)，commit b3e189c'
    e['last_checked'] = '2026-10-20'
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('verdict recorded')