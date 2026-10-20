import subprocess, os, json, datetime
FJ = r'C:/Users/37533/Desktop/workspace/焚诀/eval'
EVAL = FJ + r'/skill_hitrate_eval_v2.py'
print('eval script exists:', os.path.isfile(EVAL), '| size:', os.path.getsize(EVAL) if os.path.isfile(EVAL) else 0)
# 1) 模拟冻结命令（python -X utf8 跑通性，输出重定向到临时文件）
tmp = 'D:/workdesktop/xtmp/_rehearse_hit.txt'
r = subprocess.run(['python', '-X', 'utf8', EVAL], capture_output=True, cwd=FJ, timeout=600)
out = (r.stdout + r.stderr).decode('utf-8', 'replace')
print('eval rc:', r.returncode, '| output chars:', len(out))
open(tmp, 'w', encoding='utf-8', newline='').write(out)
# 2) 验码断言（05 §四补条的铁律）
b = open(tmp, 'rb').read()
assert b[:2] != bytes([0xFF, 0xFE]), 'UTF-16 BOM detected!'
t = b.decode('utf-8')
assert '混合路由' in t, '中文不可读=编码错'
print('ENCODING-OK:', len(t), 'chars | 混合路由 present:', '混合路由' in t)
# 3) 抽关键读数（确认与冻结基线同口径可比）
for pat in ('Top-1', 'contaminated', 'n=89', 'Wilson'):
    hits = [l.strip()[:110] for l in t.splitlines() if pat in l][:1]
    for h in hits: print('  [%s] %s' % (pat, h))