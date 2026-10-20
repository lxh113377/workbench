import os
# TDD 卷一：Step 1-3 英文 H4 锚
p1 = 'D:/global_skills/test-driven-development/references/upstream-agent-skills-tdd.md'
t = open(p1, encoding='utf-8').read()
P = [
  ('**Step 1 RED**：测试先行，必须失败——一写就过的测试证明不了任何事。',
   '#### Step 1: RED — Write a Failing Test\n\n测试先行，必须失败——一写就过的测试证明不了任何事。'),
  ('**Step 2 GREEN**：写让测试通过的**最小**代码，不过度设计。',
   '#### Step 2: GREEN — Make It Pass\n\n写让测试通过的**最小**代码，不过度设计。'),
  ('**Step 3 REFACTOR**：测试绿着，再改善代码（抽共享/改命名/去重/必要优化），**每步重构后都重跑测试**。',
   '#### Step 3: REFACTOR — Clean Up\n\n测试绿着，再改善代码（抽共享/改命名/去重/必要优化），**每步重构后都重跑测试**。'),
]
n = 0
for a, b in P:
    if a in t: t = t.replace(a, b, 1); n += 1
# see also -> 加一个 See Also 段
if '## See Also' not in t:
    t = t.rstrip() + '\n\n## See Also（参见）\n\n- 上游 `addyosmani/agent-skills` 同族件：`writing-plans` / `finishing-a-development-branch`；\n- 本仓相邻：`testing`（测试编写六阶段）、`verification-before-completion`（完工前证据核验）。\n'
    n += 1
open(p1,'w',encoding='utf-8').write(t)
print('tdd part1:', n, 'bytes', len(t.encode()))

# FPD：五步英文 H4 锚
p2 = 'D:/global_skills/first-principles-decomposer/references/upstream-doubt-driven.md'
t = open(p2, encoding='utf-8').read()
P2 = [
  ('### Step 1: CLAIM — 摊开站得住脚的东西', '#### Step 1: CLAIM — Surface what stands\n\n摊开站得住脚的东西'),
  ('### Step 2: EXTRACT — 取最小可审单元', '#### Step 2: EXTRACT — Smallest reviewable unit\n\n取最小可审单元'),
  ('### Step 3: DOUBT — 唤起新上下文评审者', '#### Step 3: DOUBT — Invoke the fresh-context reviewer\n\n唤起新上下文评审者'),
  ('### Step 4: RECONCILE — 把发现折回', '#### Step 4: RECONCILE — Fold findings back\n\n把发现折回'),
  ('### Step 5: STOP — 有界循环，不是递归', '#### Step 5: STOP — Bounded loop, not recursion\n\n有界循环，不是递归'),
  ('## Overview（总览）', '## Overview（总览）\n\n## Doubt-Driven Development'),
]
n = 0
for a, b in P2:
    if a in t: t = t.replace(a, b, 1); n += 1
open(p2,'w',encoding='utf-8').write(t)
print('fpd:', n, 'bytes', len(t.encode()))