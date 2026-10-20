import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'verification-before-completion':
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 裁决=吸收 依据=漂移腿RED(独有9)→吸收上游9节→复验GREEN(独有0)'
    if e['local_skill'] == 'brainstorming':
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 裁决=吸收 依据=漂移腿RED(独有8)→吸收上游7节→复验漂移GREEN(独有0)；候选腿1个ungh瞬时限流属取数失败非失活'
    e['last_checked'] = '2026-10-20'
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('watchlist verdicts recorded; entries =', len(reg['watchlist']))