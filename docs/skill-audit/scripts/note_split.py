import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] in ('brainstorming','code-review','debugging-fixing','refactoring','security-audit'):
        e['notes'] = (e.get('notes') or '') + '；2026-10-20 轮9 B3b 吸收卷迁 references/upstream-*.md（合并口径漂移腿 missing=0，主文件已回 12KB 内，commit 1890a42）'
json.dump(reg, open(p,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('notes updated')