import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'debugging-fixing':
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 裁决=吸收 依据=字节比0.586→1.434(commit f2dbf14),上游独有标题→0'
    if e['local_skill'] == 'refactoring':
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 裁决=吸收 依据=字节比0.355→1.201(commit f2dbf14),上游独有标题24→0'
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('verdicts recorded')