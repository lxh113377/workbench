import re, os
FIX = [
  ('D:/global_skills/test-driven-development/references/upstream-agent-skills-tdd.md', [
    ('## Discover the Stack First / Discover the Stack First（', '## Discover the Stack First（'),
    ('## The TDD Cycle / The TDD Cycle（', '## The TDD Cycle（'),
    ('## The Prove-It Pattern / The Prove-It Pattern（', '## The Prove-It Pattern（'),
    ('## The Test Pyramid / The Test Pyramid（', '## The Test Pyramid（'),
    ('## Test Sizes / Test Sizes (Resource Model)（', '## Test Sizes（'),
  ]),
  ('D:/global_skills/first-principles-decomposer/references/upstream-doubt-driven.md', [
    (' / ## Doubt-Driven Loop', ''),
    (' / ## Doubt-Driven Development', ''),
    (' / ### Step 1: CLAIM', ''),
    (' / ### Step 2: EXTRACT', ''),
    (' / ### Step 3: DOUBT', ''),
    (' / ### Step 4: RECONCILE', ''),
    (' / ### Step 5: STOP', ''),
  ]),
]
for path, pairs in FIX:
    t = open(path, encoding='utf-8').read()
    n = 0
    for a, b in pairs:
        if a in t:
            t = t.replace(a, b); n += 1
    open(path, 'w', encoding='utf-8').write(t)
    print(os.path.basename(path), 'reverted', n, 'bytes', len(t.encode()))
# 补：part2 的英文标题需用同一形式
p2 = 'D:/global_skills/test-driven-development/references/upstream-agent-skills-tdd-part2.md'
t = open(p2, encoding='utf-8').read()
for cn, en in [('## Decision Guide（决策 guide：什么时候写什么测试）', '## Decision Guide（什么时候写什么测试）'),
               ('## Writing Good Tests（好测试的判据 · 本地缺的五条）', '## Writing Good Tests（好测试的判据）'),
               ('## Name Tests Descriptively（命名即文档）', '## Name Tests Descriptively（命名即文档）'),
               ('## Test Anti-Patterns to Avoid（反模式清单）', '## Test Anti-Patterns to Avoid（反模式清单）'),
               ('## Browser Testing with DevTools（浏览器改动的双验证）', '## Browser Testing with DevTools（浏览器改动的双验证）'),
               ('### What to Check（运行期查什么）', '### What to Check（运行期查什么）'),
               ('### The DevTools Debugging Workflow（排障工作流）', '### The DevTools Debugging Workflow（排障工作流）'),
               ('### Security Boundaries（安全边界）', '### Security Boundaries（安全边界）'),
               ('## When to Use Subagents for Testing（何时派子代理跑测试）', '## When to Use Subagents for Testing（何时派子代理）'),
               ('## Common Rationalizations / Red Flags / Verification（借口/红旗/验证）', '## Common Rationalizations（常见借口）\n\n## Red Flags（危险信号）\n\n## Verification（验证）')]:
    if cn in t: t = t.replace(cn, en, 1)
open(p2, 'w', encoding='utf-8').write(t)
print('part2 normalized, bytes', len(t.encode()))