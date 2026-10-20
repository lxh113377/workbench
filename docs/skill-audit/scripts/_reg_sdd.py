import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
entry = {
  'local_skill': 'subagent-driven-development',
  'local_skill_md': 'D:/global_skills/subagent-driven-development/SKILL.md',
  'upstream_repo': 'obra/superpowers',
  'upstream_path': 'skills/executing-plans/SKILL.md',
  'recorded_version': '7.0.0',
  'last_checked': '2026-10-20',
  'discovery_query': 'executing plans skill agent',
  'candidates': [],
  'notes': '2026-10-20 轮1 新建（B4 自行决策落地）：吸收上游 executing-plans 20405B 内核四段（inline 理由/Rulings-not-stalls/ledger 存活/四项 stop 条件）；旧名 executing-plans 在退役名册，以本名注册。references/plan-execution-contract.md 定义 ledger 与 completion contract 格式。',
}
has = any(e['local_skill'] == 'subagent-driven-development' for e in reg['watchlist'])
if not has: reg['watchlist'].append(entry)
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('entries=', len(reg['watchlist']), 'added=', not has)
import os
print('SKILL.md bytes=', os.path.getsize(r'D:/global_skills/subagent-driven-development/SKILL.md'))
print('ref bytes=', os.path.getsize(r'D:/global_skills/subagent-driven-development/references/plan-execution-contract.md'))