import json, os, glob, datetime, subprocess
BASE = 'D:/workdesktop/docs/skill-audit'
print('=== 终验 1：四集对账（第0腿）===')
r = subprocess.run(['python', BASE + '/scripts/leg0_consistency_audit.py'], capture_output=True)
print((r.stdout + r.stderr).decode('utf-8','replace').strip()[:200], '| rc =', r.returncode)
print()
print('=== 终验 2：巡检基线（干净轮）===')
d = json.load(open(BASE + '/data/sweep-report.json', encoding='utf-8'))
from collections import Counter
dist = dict(Counter(x['overall'] for x in d['rows']))
print('分布:', dist, '| gen:', d['generated_at'])
print('污染标记:', 'rate_limit_pollution' in d, '| 抖动标记:', 'channel_jitter' in d)
red = [x['local_skill'] for x in d['rows'] if x['overall']=='RED']
print('RED:', red)
reg = json.load(open(BASE + '/data/skill-watchlist.json', encoding='utf-8'))
notes = {e['local_skill']: (e.get('notes') or '') for e in reg['watchlist']}
unadjudicated = [n for n in red if '裁决=' not in notes.get(n,'') and '裁决：' not in notes.get(n,'')]
print('未裁决 RED:', unadjudicated, '=>', 'PASS' if not unadjudicated else 'FAIL')
print()
print('=== 终验 3：矩阵新鲜度 ===')
m = json.load(open(BASE + '/data/score-matrix.json', encoding='utf-8'))
null_stars = [(s['local_skill'], c.get('repo')) for s in m['skills'] for c in s.get('candidates',[]) if c.get('stars') is None]
print('候选 stars 为 null:', len(null_stars), null_stars)
print('（null 且带 stars_note=限流说明的属已知暂缺）')
print()
print('=== 终验 4：文档链完整 ===')
docs = sorted(os.path.basename(p) for p in glob.glob(BASE + '/*.md'))
print('文档 %d 件:' % len(docs), docs)
print()
print('=== 终验 5：cron 存活 ===')
print('（另用 schedule_list 验）')