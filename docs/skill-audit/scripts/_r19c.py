import subprocess, re, glob
doc = ''
for f in glob.glob('D:/workdesktop/docs/skill-audit/*.md'):
    doc += open(f, encoding='utf-8', errors='replace').read()
ctx_shas = set()
for m in re.finditer(r'[0-9a-f]{7,8}', doc):
    ctx = doc[max(0, m.start()-30):m.end()+5]
    if ('commit' in ctx.lower()) or ('提交' in ctx) or ('GS 仓' in ctx) or (('`' + m.group(0) + '`') in doc):
        ctx_shas.add(m.group(0))
# 轮19 修正判据：不用 --format=%h 的浅窗口（300 条之外的提交被误判不存在），
# 改为逐 sha 实查（git log -1 <sha>），这才是真判据。
results = {}
for s in sorted(ctx_shas):
    g = subprocess.run(['git','log','--oneline','-1',s], capture_output=True, cwd='D:/global_skills')
    go = (g.stdout + g.stderr).decode('utf-8','replace').strip()
    w = subprocess.run(['git','log','--oneline','-1',s], capture_output=True, cwd='D:/workdesktop')
    wo = (w.stdout + w.stderr).decode('utf-8','replace').strip()
    ok_g = go and not go.lower().startswith('fatal')
    ok_w = wo and not wo.lower().startswith('fatal')
    results[s] = 'GS' if ok_g else ('WD' if ok_w else 'MISSING')
missing = [s for s, v in results.items() if v == 'MISSING']
print('语境内 sha:', len(ctx_shas), '| 实查缺失:', len(missing), missing)
for s, v in results.items():
    print('  %-10s %s' % (s, v))