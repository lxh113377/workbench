import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
V = {
  'story': '2026-10-20 轮16 裁决=吸收分叉（同源 sim 0.815，上游 zenstory-ai/oh-story-claudecode v0.9.0）：漂移 RED 系上游 7 个独有标题未搬（书目录判定/交给哪个助手等）；本地 story 已是入口路由件且体量 9KB＜上游 13.8KB，判定=按需增量吸收，下轮若上游新增与本地路由冲突再全量对齐；不做全文镜像（避免入口件臃肿）。',
  'story-cover': '2026-10-20 轮16 裁决=弃用候选（候选池条目）：唯一候选 danderfer/Comp_Sci_Sem_2 已 1296 天（3.5 年）未推送，远超 400 天活跃窗=真失活；按 05 §五「失活候选不得作为迁移目标」处置=标记弃用，story-cover 保持本地自研，不替换。',
  'story-long-write': '2026-10-20 轮16 裁决=弃用候选（候选池条目）：同上，danderfer/Comp_Sci_Sem_2 已 1296 天未推送=真失活；处置=标记弃用，本地技能保持自研，不替换。',
}
n = 0
for e in reg['watchlist']:
    if e['local_skill'] in V:
        e['notes'] = (e.get('notes') or '') + ' | ' + V[e['local_skill']]
        n += 1
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('verdicts recorded:', n)