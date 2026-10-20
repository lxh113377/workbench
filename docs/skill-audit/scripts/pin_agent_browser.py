import json, urllib.request, re
UA='Mozilla/5.0'
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'agent-browser':
        e['upstream_repo'] = 'vercel-labs/agent-browser'
        e['upstream_path'] = 'skill-data/core/SKILL.md'
        e['upstream_role'] = 'same_origin_upstream(轮11钉回: 并行会话 ff6fb8c8 自述对齐 v0.39.1 skill-data/core；本地38626B vs 上游38018B，差异=两处已留痕本地分叉 fork-provenance.md)'
        e['recorded_version'] = 'v0.39.1'
        e['last_checked'] = '2026-10-20'
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 轮11 血缘钉回+重基线 v0.39.1（此前登记为相邻件 browser-use）'
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('agent-browser pinned to vercel-labs/agent-browser skill-data/core v0.39.1')