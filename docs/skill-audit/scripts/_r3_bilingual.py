import os, re
NL = chr(10)
FIX = {
  'D:/global_skills/test-driven-development/references/upstream-agent-skills-tdd.md': [
    ('## Discover the Stack First（先探栈，再写第一个测试）', '## Discover the Stack First'),
    ('## The TDD Cycle（循环细步）', '## The TDD Cycle'),
    ('## The Prove-It Pattern（修 bug 的先证后修）', '## The Prove-It Pattern'),
    ('## The Test Pyramid（测试金字塔）', '## The Test Pyramid'),
    ('## Test Sizes（测试尺寸/资源模型）', '## Test Sizes'),
  ],
  'D:/global_skills/first-principles-decomposer/references/upstream-doubt-driven.md': [
    ('## 五步闭环（CLAIM → EXTRACT → DOUBT → RECONCILE → STOP）', '## Doubt-Driven Loop'),
  ],
}
for path, pairs in FIX.items():
    t = open(path, encoding='utf-8').read()
    n = 0
    for cn, en in pairs:
        if cn in t and en not in t:
            t = t.replace(cn, cn + ' / ' + en, 1)
            n += 1
    open(path, 'w', encoding='utf-8').write(t)
    print(os.path.basename(path), 'bilingual headings added:', n, 'bytes', len(t.encode()))