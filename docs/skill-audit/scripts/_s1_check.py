import json, os, ast, re, sys
d = r'D:/global_skills/subagent-driven-development'
ok = True
# S1 结构自检：frontmatter 四键 + 各 .md <=4096B + 无 .py 噪声
txt = open(os.path.join(d, 'SKILL.md'), encoding='utf-8').read()
m = re.match(r'^---\n(.*?)\n---\n', txt, re.S)
keys = set(re.findall(r'^(\w+):', m.group(1), re.M)) if m else set()
need = {'name', 'description', 'version', 'triggers'}
print('frontmatter keys:', sorted(keys), 'ok=', need.issubset(keys))
ok = ok and need.issubset(keys)
for root, dirs, files in os.walk(d):
    for f in files:
        p = os.path.join(root, f)
        sz = os.path.getsize(p)
        if f.endswith('.md'):
            print('%-42s %6d B  <=4096: %s' % (os.path.relpath(p, d), sz, sz <= 4096))
            ok = ok and sz <= 4096
        else:
            print('NON-MD FILE:', os.path.relpath(p, d), sz)
            ok = ok and False
print('STRUCTURE_SELFTEST', 'PASS' if ok else 'FAIL')
sys.exit(0 if ok else 1)