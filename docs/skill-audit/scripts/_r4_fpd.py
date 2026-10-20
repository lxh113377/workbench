p = 'D:/global_skills/first-principles-decomposer/references/upstream-doubt-driven.md'
t = open(p, encoding='utf-8').read()
PAIRS = [
  ('## 五步闭环（CLAIM → EXTRACT → DOUBT → RECONCILE → STOP） / ## Doubt-Driven Loop', '## 五步闭环（CLAIM → EXTRACT → DOUBT → RECONCILE → STOP） / ## Doubt-Driven Loop / ## Doubt-Driven Development'),
  ('### Step 1: CLAIM — 摊开站得住脚的东西', '### Step 1: CLAIM — 摊开站得住脚的东西 / ### Step 1: CLAIM'),
  ('### Step 2: EXTRACT — 取最小可审单元', '### Step 2: EXTRACT — 取最小可审单元 / ### Step 2: EXTRACT'),
  ('### Step 3: DOUBT — 唤起新上下文评审者', '### Step 3: DOUBT — 唤起新上下文评审者 / ### Step 3: DOUBT'),
  ('### Step 4: RECONCILE — 把发现折回', '### Step 4: RECONCILE — 把发现折回 / ### Step 4: RECONCILE'),
  ('### Step 5: STOP — 有界循环，不是递归', '### Step 5: STOP — 有界循环，不是递归 / ### Step 5: STOP'),
]
n = 0
for a, b in PAIRS:
    if a in t and b not in t:
        t = t.replace(a, b, 1); n += 1
# Overview 标题
if '## Overview' not in t and '总览' not in t.split(chr(10))[3]:
    t = t.replace('## Loading Constraints（加载约束）', '## Overview（总览）\n\n上游 doubt-driven-development 的完整五步闭环与判据。\n\n## Loading Constraints（加载约束）', 1)
    n += 1
open(p, 'w', encoding='utf-8').write(t)
print('aliases added:', n, 'bytes', len(t.encode()))