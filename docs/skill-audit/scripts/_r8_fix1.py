import json, datetime
P = r'D:/workdesktop/docs/skill-audit/data/score-matrix.json'
m = json.load(open(P, encoding='utf-8'))
reg = json.load(open(r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json', encoding='utf-8'))
ent = {e['local_skill']: e for e in reg['watchlist']}
# 1) 删 executing-plans 残留（与 watchlist 对齐）
before = len(m['skills'])
m['skills'] = [s for s in m['skills'] if s['local_skill'] != 'executing-plans']
print('removed stale matrix entries:', before - len(m['skills']))
# 2) sdd 候选同步 watchlist（4 个并入的）
sdd_e = ent['subagent-driven-development']
sdd_m = next((s for s in m['skills'] if s['local_skill']=='subagent-driven-development'), None)
wrep = sdd_e.get('candidates') or []
print('watchlist sdd candidates:', len(wrep))
print('matrix sdd candidates before:', len(sdd_m.get('candidates') or []))
# 按 watchlist 重建 sdd 候选（保留原 batch-E 的 F/E/C/D/S，未取到的记 null）
def band(v, bands, default=None):
    if v is None: return default
    for lo, sc in bands:
        if v >= lo: return sc
    return default
cands = []
for c in wrep:
    st = c.get('stars')
    S = band(st, [(100000,5),(10000,4),(1000,3),(100,2),(0,1)]) if st is not None else None
    cands.append({
      'repo': c['repo'], 'evidence': c.get('entry_path'), 'stars': st,
      'license': c.get('license'), 'last_activity': None,
      'match_reason': '轮7 从 executing-plans 旧条目交接并入（05 §十八）' if c.get('merged_from') else '轮1 血缘钉回',
      'confidence': 'medium' if c.get('merged_from') else 'high',
      'ubytes': None, 'ratio_up_over_local': None,
      'F': None, 'E': None, 'C': None, 'D': None, 'S': S,
      'total_measured': (S if S is not None else None), 'dims_measured': (1 if S is not None else 0),
    })
sdd_m['candidates'] = cands
sdd_m['candidate_count'] = len(cands)
best = max((c for c in cands if c['total_measured'] is not None), key=lambda x: x['total_measured'])
sdd_m['best'] = {'repo': best['repo'], 'total': best['total_measured']}
print('matrix sdd candidates after:', len(cands))
json.dump(m, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('matrix skills now:', len(m['skills']))