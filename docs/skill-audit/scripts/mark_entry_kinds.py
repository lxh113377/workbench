import json, datetime
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
CAND = {
 'A-prompt-better','canvas-design','cloudbase','coding-agent','data-analysis','dingtalk-chat','discover-agent-cli',
 'figma','find-skills','frontend-skill','hyperframes','knowledge-capture','local-asr','local-computer-use',
 'mcp-builder','pdf','rag-eval','shadcn','spec-to-implementation','story-cover','story-long-write',
 'workbuddy-mcp','xlsx'}
SELFW = {'gstack','ppt-master','A-ask-questions','A-project-better','screenshot','office-automation-pro','A-prompt-better2'}
n_c = n_s = 0
for e in reg['watchlist']:
    name = e['local_skill']
    if name in CAND:
        e['entry_kind'] = 'candidate_pool(候选池：同名/同族入口件未定位，不等于本技能上游)'
        e['upstream_role'] = (e.get('upstream_role') or '') or 'candidate_pool(batch-C 检索 STRONG 候选，入口件未定位)'
        n_c += 1
    elif not e.get('upstream_repo'):
        e['entry_kind'] = 'self_developed(自研：无外部上游可追踪，巡检不判版本)'
        n_s += 1
reg['_policy_notes'] = (reg.get('_policy_notes') or '') + ' | 2026-10-20 轮2：entry_kind 三分法——same_origin_upstream(同源上游，漂移腿判)/candidate_pool(候选池，仅候选活性腿)/self_developed(自研，版本腿跳过)。依据：batch-C 提词检索的 STRONG 候选与本地技能名常无对应（xlsx→genoffice 套件、pdf→seo-* 族），硬填 upstream_path=造假血缘。'
json.dump(reg, open(P,'w',encoding='utf-8'), ensure_ascii=False, indent=2)
print('candidate_pool marked:', n_c, '| self_developed marked:', n_s)