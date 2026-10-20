import os
NL = chr(10)
PLAN = {
  'test-driven-development': {
    'ref': 'references/upstream-agent-skills-tdd.md',
    'bump': '1.1.0',
    'desc': '吸收上游 addyosmani/agent-skills test-driven-development v0.6.13（2026-10-20 轮3）：先探栈/TDD循环细步/Prove-It修bug模式/测试金字塔与尺寸模型四块；测试写法通则留给 testing 不重复。漂移 RED(sim 0.756 同源)处置=吸收增量。',
  },
  'first-principles-decomposer': {
    'ref': 'references/upstream-doubt-driven.md',
    'bump': '1.1.0',
    'desc': '吸收上游 addyosmani/agent-skills doubt-driven-development v0.6.13（2026-10-20 轮3）：怀疑驱动五步闭环 CLAIM→EXTRACT→DOUBT→RECONCILE→STOP + 跨模型升级 + 反理性化清单，12 个独有标题全量。漂移 RED(sim 0.773 同源)处置=吸收增量。',
  },
  'frontend-slides': {
    'ref': 'references/upstream-frontend-slides.md',
    'bump': None,
    'desc': '吸收上游 zarazhangrui/frontend-slides（2026-10-20 轮3）：固定舞台不变量 + 内容密度模式 + Step2.0 三风格预览，4 个独有标题全量。漂移 RED(sim 0.818 同源)处置=吸收增量。',
  },
}
for name, cfg in PLAN.items():
    p = 'D:/global_skills/' + name + '/SKILL.md'
    raw = open(p,'rb').read()
    bom = raw[:3] == bytes([0xEF,0xBB,0xBF])
    t = raw.decode('utf-8-sig')
    nl = chr(13)+chr(10) if (chr(13)+chr(10)) in t else chr(10)
    lines = t.splitlines()
    if cfg['bump']:
        vi = next((i for i,l in enumerate(lines) if l.startswith('version:')), None)
        if vi is not None:
            cur = lines[vi].split(':',1)[1].strip()
            if cfg['bump'] > cur: lines[vi] = 'version: ' + cfg['bump']
    ptr = ['', '## Upstream-absorbed sections（分卷指针）', '', '受控吸收的上游内容整卷在 `%s`（%s）加载本技能时一并读取。' % (cfg['ref'], cfg['desc'])]
    if '## Version history' in t:
        hi = next(i for i,l in enumerate(lines) if l.startswith('## Version history'))
        lines = lines[:hi] + ptr + ['', '## Version history'] + lines[hi+1:]
    else:
        lines = lines + ptr
    body = nl.join(lines) + (nl if t.endswith(nl) else '')
    data = body.encode('utf-8')
    if bom: data = bytes([0xEF,0xBB,0xBF]) + data
    open(p,'wb').write(data)
    print('%-30s main=%6d' % (name, os.path.getsize(p)))