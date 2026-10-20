import json, datetime
d = json.load(open(r'D:/workdesktop/docs/skill-audit/data/batchC-search-evidence.json', encoding='utf-8'))
rows = d['queries']
def bucket(q):
    if q.get('error'): return 'ERR'
    tc = q.get('total_count')
    if tc == 0: return 'ZERO'
    top = q.get('top') or []
    strong = [t for t in top if (t.get('stars') or 0) >= 100 and not t.get('archived')]
    return 'STRONG' if strong else 'WEAK'
buckets = {}
for q in rows: buckets.setdefault(bucket(q), []).append(q)
A = []
A.append('# 08 · 全量检索台账（第 2 条「每一个现有 skill」的 repo 级闭环）')
A.append('')
A.append('> 生成 ' + datetime.datetime.now().isoformat(timespec='seconds') + '；来源 `scripts/batchC_search.py` → `data/batchC-search-evidence.json`；查询词=技能自身 description 提词+停用词过滤+` skill agent` 后缀；每技能存 total_count 与按 stars 排序 top5。')
A.append('> **读数口径**：0 命中＝GitHub 层面无同类公开仓库（合法实证，不是失败）；WEAK＝有命中但 top 无 ≥100★ 活跃仓；STRONG＝有 ≥100★ 活跃候选，下轮进矩阵补采。')
A.append('')
counts = {k: len(v) for k, v in buckets.items()}
A.append('| 桶 | 数量 | 含义 |')
A.append('|---|---|---|')
A.append('| ZERO（无同类公开仓库）| %d | 自研合理性实证 |' % counts.get('ZERO', 0))
A.append('| WEAK（命中但候选弱）| %d | 外部无可维护替代 |' % counts.get('WEAK', 0))
A.append('| STRONG（≥100★活跃候选）| %d | 下轮矩阵补采候选 |' % counts.get('STRONG', 0))
if 'ERR' in buckets: A.append('| ERR（配额/网络中断）| %d | 下轮补跑 |' % len(buckets['ERR']))
A.append('')
A.append('## STRONG 明细（进下轮候选池）')
A.append('')
A.append('| 技能 | total | 最强候选 | ★ | pushed | license |')
A.append('|---|---|---|---|---|---|')
for q in buckets.get('STRONG', []):
    t = max((x for x in q['top'] if (x.get('stars') or 0) >= 100 and not x.get('archived')), key=lambda x: x.get('stars') or 0)
    A.append('| %s | %s | %s | %s | %s | %s |' % (q['local_skill'], q.get('total_count'), t['repo'], t.get('stars'), t.get('pushed'), t.get('license') or '-'))
A.append('')
A.append('## ZERO 清单（无同类公开仓库＝自研合理性）')
A.append('')
A.append(', '.join(sorted(q['local_skill'] for q in buckets.get('ZERO', []))))
A.append('')
A.append('## WEAK 清单')
A.append('')
A.append(', '.join(sorted(q['local_skill'] for q in buckets.get('WEAK', []))))
A.append('')
open(r'D:/workdesktop/docs/skill-audit/08-全量检索台账.md','w',encoding='utf-8').write(chr(10).join(A))
print('ledger written:', counts)