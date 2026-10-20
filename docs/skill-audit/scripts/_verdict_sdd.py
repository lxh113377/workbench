import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'subagent-driven-development':
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 轮1 裁决=吸收分叉（05 §五）：漂移腿 RED 系本地中文精选吸收(3846B)对上游全文(20405B)的预期差异，上游独有段按需增量吸收，不做全文镜像；版本腿 GREEN(7.0.0)。S1 结构自检 PASS(3 卷全 ≤4096B)、冒烟 PASS(分卷无死链/新名不在退役册)。'
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('verdict recorded')