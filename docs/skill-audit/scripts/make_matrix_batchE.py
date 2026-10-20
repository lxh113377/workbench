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
name = 'subagent-driven-development'
lb = local_bytes.get(name) or os.path.getsize('D:/global_skills/subagent-driven-development/SKILL.md')
up_repo, up_path = 'obra/superpowers', 'skills/executing-plans/SKILL.md'
up_bytes, up_stars = 20405, 302151   # 轮1 实测 raw 20405B；obra/superpowers stars 轮12 实测
ratio = round(up_bytes / lb, 3) if (up_bytes and lb) else None
F = max(1, min(5, 3 + (2 if ratio >= 1.5 else 1 if ratio >= 1.0 else -1 if ratio < 0.6 else 0))) if ratio else None
E = band(up_bytes, [(65536, 1), (32768, 2), (16384, 3), (8192, 4), (0, 5)])
C = 4   # MIT 宽松许可
D = band(up_bytes, [(15360, 5), (8192, 4), (3072, 3), (0, 2)])
S = band(up_stars, [(100000, 5), (10000, 4), (1000, 3), (100, 2), (0, 1)])
vals = [v for v in (F, E, C, D, S) if v is not None]
entry = {
  'repo': up_repo, 'evidence': up_path, 'stars': up_stars, 'license': 'MIT',
  'last_activity': '2026-10-10',
  'match_reason': '轮1 血缘钉回：本地新技能即吸收该上游内核四段（B4 自行决策落地）',
  'confidence': 'high', 'ubytes': up_bytes, 'ratio_up_over_local': ratio,
  'F': F, 'E': E, 'C': C, 'D': D, 'S': S,
  'total_measured': sum(vals), 'dims_measured': len(vals),
}
out = {'schema': 'skill-score-matrix-v1', 'batch': 'batch-E',
       'generated_at': datetime.datetime.now().isoformat(timespec='seconds'),
       'rules': {'F': '入口件/本地字节比：3+(>=1.5:+2/>=1.0:+1/<0.6:-1)', 'E': '入口件字节：<=8K:5 <=16K:4 <=32K:3 <=64K:2 else 1',
                 'C': 'SKILL.md 格式兼容 3；宽松许可+1', 'D': '入口件字节：>=15K:5 >=8K:4 >=3K:3 else 2',
                 'S': 'stars：>=100k:5 >=10k:4 >=1k:3 >=100:2 else 1；未取证记 null'},
       'baseline_date': NOW.isoformat(),
       'skills': [{'local_skill': name, 'local_bytes': lb, 'remote_note': 'batch-E（轮4：轮1 新建技能补入矩阵）',
                    'candidates': [entry], 'candidate_count': 1,
                    'best': {'repo': up_repo, 'total': sum(vals)}}]}
json.dump(out, open(BASE + '/data/score-matrix-batchE.json', 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('local_bytes=%d up_bytes=%d ratio=%s F/E/C/D/S=%s/%s/%s/%s/%s total=%s' % (lb, up_bytes, ratio, F, E, C, D, S, sum(vals)))