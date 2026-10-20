import subprocess, json, re, glob
def sh(args, cwd):
    r = subprocess.run(args, capture_output=True, cwd=cwd)
    return (r.stdout + r.stderr).decode('utf-8', errors='replace')
print('=== Dual-repo commit consistency ===')
gs = sh(['git','log','--oneline','-15'], 'D:/global_skills').strip().splitlines()
print('GS repo last 15:')
for l in gs:
    print('  ', l[:95])
wd = sh(['git','log','--oneline','-15'], 'D:/workdesktop').strip().splitlines()
print()
print('workdesktop last 15:')
for l in wd:
    print('  ', l[:95])
doc = ''
for f in glob.glob('D:/workdesktop/docs/skill-audit/*.md'):
    doc += open(f, encoding='utf-8', errors='replace').read()
shas = set(re.findall(r'[0-9a-f]{7,8}', doc))
gs_all = sh(['git','log','--format=%h','-300'], 'D:/global_skills')
gs_shas = set(gs_all.split())
missing = sorted(s for s in shas if s not in gs_shas)
print()
print('doc-referenced shas:', len(shas), '| not in GS repo:', len(missing))
print('  sample:', missing[:12])