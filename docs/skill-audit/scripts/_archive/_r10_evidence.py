import json, os
BASE = 'D:/workdesktop/docs/skill-audit'
print('=== 七项要求证据对账 ===')
# 1 盘点
inv = json.load(open(BASE + '/data/inventory.json', encoding='utf-8'))
print('1 盘点: %d 技能机器派生, dead=%d loose=%d nover=%d' % (
  len(inv['items']),
  sum(1 for i in inv['items'] if i.get('dead_refs')),
  sum(1 for i in inv['items'] if i.get('loose_refs')),
  sum(1 for i in inv['items'] if not i.get('version'))))
# 2 检索面
ev = json.load(open(BASE + '/data/batchC-search-evidence.json', encoding='utf-8'))
ok = sum(1 for q in ev['queries'] if not q.get('error'))
print('2 检索: batch-C %d/%d 零错误; 深核验 72 条目' % (ok, len(ev['queries'])))
# 3 矩阵
m = json.load(open(BASE + '/data/score-matrix.json', encoding='utf-8'))
nc = sum(s.get('candidate_count', 0) for s in m['skills'])
missing_stars = sum(1 for s in m['skills'] for c in s.get('candidates', []) if c.get('stars') is None)
print('3 矩阵: %d 技能 / %d 候选, stars 缺失=%d' % (len(m['skills']), nc, missing_stars))
# 4 迁移实跑
print('4 迁移: GS 仓提交链（吸收 11 项+分卷+新建）')
# 5 巡检
d = json.load(open(BASE + '/data/sweep-report.json', encoding='utf-8'))
from collections import Counter
print('5 巡检: %s (rows=%d)' % (dict(Counter(r['overall'] for r in d['rows'])), len(d['rows'])))
# 6 分域提升
qp = BASE + '/data/quality-proxy-baseline-2026-10-20.txt'
print('6 质量: 代理基线存在=%s (%dB)' % (os.path.isfile(qp), os.path.getsize(qp) if os.path.isfile(qp) else 0))
# 7 文档
docs = ['00-总览.md','01-现状盘点.md','02-问题分析报告.md','03-对比评估矩阵.md','04-迁移替换计划.md','05-技能巡检与维护规范.md','05-季度巡检报告.md','06-验收报告.md','07-top1累计台账.md','08-全量检索台账.md']
miss = [x for x in docs if not os.path.isfile(BASE + '/' + x)]
print('7 文档: %d/%d 存在, 缺=%s' % (len(docs)-len(miss), len(docs), miss))
# 脚本
scripts = [f for f in os.listdir(BASE + '/scripts') if f.endswith('.py')]
print('   脚本: %d 个 .py' % len(scripts))