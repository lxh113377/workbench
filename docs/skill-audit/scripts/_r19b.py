import subprocess, re, glob
doc = ''
for f in glob.glob('D:/workdesktop/docs/skill-audit/*.md'):
    doc += open(f, encoding='utf-8', errors='replace').read()
# 精确判据：只匹配 commit 语境（'commit '、'提交 '、'`' 前后 20 字符内的 7-8 位纯小写十六进）
ctx_shas = set()
for m in re.finditer(r'[0-9a-f]{7,8}', doc):
    ctx = doc[max(0, m.start()-30):m.end()+5]
    if ('commit' in ctx.lower()) or ('提交' in ctx) or ('GS 仓' in ctx) or (m.group(0) in doc and ('`' + m.group(0) + '`') in doc):
        ctx_shas.add(m.group(0))
r = subprocess.run(['git','log','--format=%h','-300'], capture_output=True, cwd='D:/global_skills')
gs = set((r.stdout + r.stderr).decode('utf-8','replace').split())
r2 = subprocess.run(['git','log','--format=%h','-300'], capture_output=True, cwd='D:/workdesktop')
wd = set((r2.stdout + r2.stderr).decode('utf-8','replace').split())
both = gs | wd
missing = sorted(s for s in ctx_shas if s not in both)
print('语境内引用的 sha:', len(ctx_shas))
print('双仓都不存在的:', len(missing), missing[:10])
# 逐个人工核对（少量）
for s in sorted(ctx_shas)[:20]:
    where = 'GS' if s in gs else ('WD' if s in wd else 'NONE')
    print('  %-10s %s' % (s, where))