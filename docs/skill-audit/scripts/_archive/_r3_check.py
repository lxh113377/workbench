p = 'D:/global_skills/test-driven-development/references/upstream-agent-skills-tdd.md'
raw = open(p,'rb').read()
print('bom:', raw[:3] == bytes([0xEF,0xBB,0xBF]))
lines = raw.decode('utf-8-sig').splitlines()
for i, l in enumerate(lines):
    if l.startswith('## ') and 'Version' not in l:
        print(i, repr(l))