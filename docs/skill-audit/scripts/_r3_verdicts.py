import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
V = {
  'test-driven-development': '2026-10-20 轮3 裁决=吸收增量（sim 0.756 同源成立）：上游四块入 references/upstream-agent-skills-tdd.md；写法通则留 testing 不重复',
  'first-principles-decomposer': '2026-10-20 轮3 裁决=吸收增量（sim 0.773 同源成立）：上游 doubt-driven 五步闭环 12 标题全量入 references/upstream-doubt-driven.md',
  'A-dynamic-workflow': '2026-10-20 轮3 裁决=吸收增量（sim 0.555 同源成立）：上游 multi-reviewer-patterns 12 标题入 references/upstream-multi-reviewer-patterns.md，版本 1.3.0→1.4.0',
  'frontend-slides': '2026-10-20 轮3 裁决=吸收增量（sim 0.818 同源成立）：上游舞台不变量+密度模式 4 标题入 references/upstream-frontend-slides.md',
}
n = 0
for e in reg['watchlist']:
    if e['local_skill'] in V:
        e['notes'] = (e.get('notes') or '') + ' | ' + V[e['local_skill']]
        n += 1
json.dump(reg, open(P,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('verdicts recorded:', n)