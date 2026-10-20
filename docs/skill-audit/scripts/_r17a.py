import json, os, glob, datetime
BASE = 'D:/workdesktop/docs/skill-audit'
now = datetime.datetime.now()
print('=== 新鲜度审计（与上轮不同角度：隐性腐坏）===')
# 1) 各数据文件 mtime vs 最后一次 git 提交
import subprocess
last_commit = subprocess.run(['git','-C','D:/workdesktop','log','-1','--format=%ct'], capture_output=True).stdout.decode().strip()
lc = datetime.datetime.fromtimestamp(int(last_commit))
print('last commit:', lc.strftime('%m-%d %H:%M'))
for f in ['inventory.json','skill-watchlist.json','score-matrix.json','sweep-report.json']:
    p = BASE + '/data/' + f
    mt = datetime.datetime.fromtimestamp(os.path.getmtime(p))
    age_min = (now - mt).total_seconds() / 60
    after_commit = mt > lc
    print('  %-24s mtime=%s  %5.0f 分钟前  新于提交:%s' % (f, mt.strftime('%H:%M'), age_min, after_commit))
print()
# 2) watchlist 内部：last_checked 全部是今天？
reg = json.load(open(BASE + '/data/skill-watchlist.json', encoding='utf-8'))
stale = [(e['local_skill'], e.get('last_checked')) for e in reg['watchlist'] if (e.get('last_checked') or '') < '2026-10-20']
print('last_checked 早于今天的条目:', len(stale), stale[:10])
print()
# 3) 文档与数据的一致性（00/06 的读数 vs 实测）
inv = json.load(open(BASE + '/data/inventory.json', encoding='utf-8'))
m = json.load(open(BASE + '/data/score-matrix.json', encoding='utf-8'))
print('实测: inv=%d mx=%d' % (len(inv['items']), len(m['skills'])))
for doc in ['00-总览.md','06-验收报告.md']:
    t = open(BASE + '/' + doc, encoding='utf-8').read()
    import re
    hits = re.findall(r'(\d+)\s*技能', t)[:6]
    print('  %s 中出现的「N 技能」: %s' % (doc, hits))