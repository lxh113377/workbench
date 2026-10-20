# TDD part2: 五条判据与小节标题需与上游 H4 字面一致（上游用 #### One Assertion Per Concept 等）
p = 'D:/global_skills/test-driven-development/references/upstream-agent-skills-tdd-part2.md'
t = open(p, encoding='utf-8').read()
PAIRS = [
  ('1. **Test State, Not Interactions** — 断言**状态与输出**，不断言内部调用链（断言交互=测试被实现细节绑死，重构即红）；',
   '#### Test State, Not Interactions\n\n断言**状态与输出**，不断言内部调用链（断言交互=测试被实现细节绑死，重构即红）；'),
  ('2. **DAMP Over DRY in Tests** — 测试里**可接受重复**。为了 DRY 把测试抽成高度参数化，失败时就看不出是哪个场景坏了；',
   '#### DAMP Over DRY in Tests\n\n测试里**可接受重复**。为了 DRY 把测试抽成高度参数化，失败时就看不出是哪个场景坏了；'),
  ('3. **Prefer Real Implementations Over Mocks** — 能写真实现就写。Mock 只留给外部边界（网络/时钟/第三方），且 mock 越多，测试证明力越弱；',
   '#### Prefer Real Implementations Over Mocks\n\n能写真实现就写。Mock 只留给外部边界（网络/时钟/第三方），且 mock 越多，测试证明力越弱；'),
  ('4. **Use the Arrange-Act-Assert Pattern** — 三段式：准备（Arrange）→ 触发（Act）→ 断言（Assert）。一眼看出测试在验什么；',
   '#### Use the Arrange-Act-Assert Pattern\n\n三段式：准备（Arrange）→ 触发（Act）→ 断言（Assert）。一眼看出测试在验什么；'),
  ('5. **One Assertion Per Concept** — 一个测试验**一个概念**。多个断言堆在一起，第一个失败就挡住后面所有信息。',
   '#### One Assertion Per Concept\n\n一个测试验**一个概念**。多个断言堆在一起，第一个失败就挡住后面所有信息。'),
  ('- 断言私有实现细节 / 内部调用次数；', '#### Test Anti-Patterns to Avoid\n\n- 断言私有实现细节 / 内部调用次数；'),
]
n = 0
for a, b in PAIRS:
    if a in t: t = t.replace(a, b, 1); n += 1
open(p,'w',encoding='utf-8').write(t)
print('part2 headings added:', n, 'bytes', len(t.encode()))
# 移除重复的 Test Anti-Patterns 标题（原 ## 级）
t = open(p, encoding='utf-8').read()
t = t.replace('## Test Anti-Patterns to Avoid（反模式清单）\n\n#### Test Anti-Patterns to Avoid', '## Test Anti-Patterns to Avoid（反模式清单）', 1)
open(p,'w',encoding='utf-8').write(t)
print('dedup ok, bytes', len(t.encode()))