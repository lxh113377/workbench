import json
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
RECLASS = ['spike','A-skill-manager','html-ppt','A-project-handoff','computer-use-guidance-windows','docx','A-memory-start','audit-runner-safe-aggregate','brand-guidelines','chart-visualization','data-visualization','oracle','video-frames','wechat-automation']
KEEP = {'testing','ci-pipeline-debugging','subagent-driven-development','test-driven-development'}
print('KEEP (已裁决/高相似同源，不动):', sorted(KEEP))
n = 0
for e in reg['watchlist']:
    name = e['local_skill']
    if name in RECLASS:
        old_kind = e.get('entry_kind') or 'same_origin_upstream(缺省)'
        e['entry_kind'] = 'candidate_pool(轮3 重分类: 漂移相似度<0.5 或上游独有>=20 ⇒ 上游非本地同源件，自动选品伪同源)'
        e['upstream_role'] = (e.get('upstream_role') or '') or 'candidate_pool(轮3 重分类，非同源)'
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 轮3 裁决=重分类为候选池（漂移腿红因=上游非同源：相似度/独有标题客观判据命中，上游件与本地技能非同一物；判据见 05 §十五+§十六）'
        n += 1
json.dump(reg, open(P,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('reclassified:', n)
kinds = {}
for e in reg['watchlist']:
    k = (e.get('entry_kind') or 'NONE')[:22]
    kinds[k] = kinds.get(k,0)+1
print(kinds)