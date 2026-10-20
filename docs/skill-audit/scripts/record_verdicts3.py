import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'code-review':
        e['notes'] = (e.get('notes') or '') + '；复扫 08:18 总判定 GREEN'
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('ok')