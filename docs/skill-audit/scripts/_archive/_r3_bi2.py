p = 'D:/global_skills/test-driven-development/references/upstream-agent-skills-tdd.md'
raw = open(p,'rb').read()
t = raw.decode('utf-8-sig')
# 每个中文标题行尾追加「/ <English canonical>」，用行级处理避免子串误判
PAIRS = {
  '## Discover the Stack First': '## Discover the Stack First / Discover the Stack First',
  '## The TDD Cycle': '## The TDD Cycle / The TDD Cycle',
  '## The Prove-It Pattern': '## The Prove-It Pattern / The Prove-It Pattern',
  '## The Test Pyramid': '## The Test Pyramid / The Test Pyramid',
  '## Test Sizes': '## Test Sizes / Test Sizes (Resource Model)',
}
lines = t.splitlines()
n = 0
for i, l in enumerate(lines):
    for k, v in PAIRS.items():
        if l.startswith(k) and ' / ' not in l:
            lines[i] = v + l[len(k):]
            n += 1
            break
open(p, 'w', encoding='utf-8').write(chr(10).join(lines) + chr(10))
print('bilingual added:', n)
for l in lines:
    if l.startswith('## ') and 'Version' not in l: print('  ', l[:70])