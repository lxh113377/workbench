import json, datetime, os
BASE = 'D:/workdesktop/docs/skill-audit'
NOW = datetime.date(2026, 10, 20)
def band(v, bands, default=None):
    if v is None: return default
    for lo, sc in bands:
        if v >= lo: return sc
    return default
inv = json.load(open(BASE + '/data/inventory.json', encoding='utf-8'))
local_bytes = {i['name']: i.get('skill_md_bytes') for i in inv['items'] if i.get('has_skill_md')}
ver = json.load(open(BASE + '/data/batchD-entry-verify.json', encoding='utf-8'))
def sc(rec):
    local = rec['local_skill']; lb = local_bytes.get(local); ub = rec.get('bytes')
    ratio = round(ub / lb, 3) if (ub and lb) else None
    F = None
    if ratio is not None:
        F = max(1, min(5, 3 + (2 if ratio >= 1.5 else 1 if ratio >= 1.0 else -1 if ratio < 0.6 else 0)))
    E = band(ub, [(65536, 1), (32768, 2), (16384, 3), (8192, 4), (0, 5)])
    lic = (rec.get('license') or '').upper()
    C = max(1, min(5, 3 + (1 if lic in ('MIT','APACHE-2.0','BSD-3-CLAUSE','ISC') else (-1 if (lic in ('CC-BY-SA-4.0','GPL-3.0','AGPL-3.0','NOASSERTION') or not lic) else 0))))
    D = band(ub, [(15360, 5), (8192, 4), (3072, 3), (0, 2)])
    S = band(rec.get('stars'), [(100000, 5), (10000, 4), (1000, 3), (100, 2), (0, 1)])
    age = None
    if rec.get('pushed'):
        try: age = (NOW - datetime.date.fromisoformat(str(rec['pushed'])[:10])).days
        except ValueError: age = None
    if S is not None and age is not None and age > 180: S = max(1, S - 1)
    vals = [v for v in (F, E, C, D, S) if v is not None]
    return {'repo': rec['repo'], 'evidence': rec.get('resolved_path'), 'stars': rec.get('stars'),
            'license': rec.get('license'), 'last_activity': rec.get('pushed'),
            'match_reason': 'batch-C 检索 STRONG 候选，入口件%s' % ('实测 HTTP 200' if rec.get('http') == 200 else '未定位（保留 stars/活跃读数）'),
            'confidence': 'high' if rec.get('http') == 200 else 'low',
            'ubytes': ub, 'ratio_up_over_local': ratio, 'F': F, 'E': E, 'C': C, 'D': D, 'S': S,
            'total_measured': (sum(vals) if vals else None), 'dims_measured': len(vals)}
skills = []
seen = set()
for rec in ver['checked']:
    if rec['local_skill'] in seen: continue
    seen.add(rec['local_skill'])
    skills.append({'local_skill': rec['local_skill'], 'local_bytes': local_bytes.get(rec['local_skill']),
                   'remote_note': 'batch-D（STRONG 桶最强候选，repo 级实测）',
                   'candidates': [sc(rec)]})
for s in skills:
    s['candidate_count'] = len(s['candidates'])
    b = s['candidates'][0]
    s['best'] = {'repo': b['repo'], 'total': b['total_measured']}
out = {'schema': 'skill-score-matrix-v1', 'batch': 'batch-D',
       'generated_at': datetime.datetime.now().isoformat(timespec='seconds'),
       'rules': {'F': '入口件/本地字节比：3+(>=1.5:+2/>=1.0:+1/<0.6:-1)', 'E': '入口件字节：<=8K:5 <=16K:4 <=32K:3 <=64K:2 else 1',
                 'C': 'SKILL.md 格式兼容 3；宽松许可+1；CC-BY-SA/GPL/无声明-1', 'D': '入口件字节：>=15K:5 >=8K:4 >=3K:3 else 2',
                 'S': 'stars：>=100k:5 >=10k:4 >=1k:3 >=100:2 else 1；>180天未活跃 -1；未取证记 null'},
       'baseline_date': NOW.isoformat(), 'skills': skills}
json.dump(out, open(BASE + '/data/score-matrix-batchD.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('skills=%d' % len(skills))
for s in skills[:40]:
    print('  %-28s best=%s total=%s' % (s['local_skill'], s['best']['repo'], s['best']['total']))