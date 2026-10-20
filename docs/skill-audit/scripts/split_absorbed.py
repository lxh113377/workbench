import os, re, json, hashlib
ROOT = 'D:/global_skills'
PLAN = {
  'brainstorming': dict(start='## First: What Do You Actually Know?', end_regex=r'^## Anti-Pattern', dest='references/upstream-superpowers-v7.md', mid=True),
  'code-review': dict(start='# Code Review and Quality（吸收上游', dest='references/upstream-agent-skills.md', mid=False),
  'debugging-fixing': dict(start='# Debugging and Error Recovery（吸收上游', dest='references/upstream-agent-skills.md', mid=False),
  'refactoring': dict(start='# Code Simplification（吸收上游', dest='references/upstream-agent-skills.md', mid=False),
  'security-audit': dict(start='# Agent OWASP ASI Compliance Check（吸收上游', dest='references/upstream-owasp-asi.md', mid=False),
}
report = {}
for name, cfg in PLAN.items():
    p = os.path.join(ROOT, name, 'SKILL.md')
    raw = open(p, 'rb').read()
    bom = raw[:3] == b'\xef\xbb\xbf'
    text = raw.decode('utf-8-sig')
    nl = '\r\n' if '\r\n' in text else '\n'
    lines = text.splitlines()
    si = None
    for i, l in enumerate(lines):
        if l.startswith(cfg['start']):
            si = i; break
    if si is None:
        report[name] = 'SKIP: start marker not found'; continue
    if cfg.get('mid'):
        ei = None
        for j in range(si + 1, len(lines)):
            if re.match(cfg['end_regex'], lines[j]):
                ei = j; break
        if ei is None:
            report[name] = 'SKIP: end marker not found'; continue
    else:
        ei = len(lines)
    block = lines[si:ei]
    # trim trailing blank lines of block for clean move
    while block and not block[-1].strip():
        block = block[:-1]
    block_text = nl.join(block)
    head = ('> 本卷为 %s 的上游吸收内容分卷（B3b 分卷化 2026-10-20，原块自 SKILL.md L%d 迁出，逐字节保留）。' % (name, si + 1) + nl +
            '> 漂移腿对账口径 = 主文件 + references/ 全部 .md 合并（quarterly_skill_sweep.load_local_merged）。' + nl + nl)
    dest = os.path.join(ROOT, name, cfg['dest'])
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    open(dest, 'wb').write((head + block_text + nl).encode('utf-8'))
    ptr = ('## Upstream-absorbed sections（分卷指针）' + nl + nl +
           '受控吸收的上游内容整卷迁至 `%s`（加载本技能时一并读取；卷内含吸收块全文与其 Version history）。' % cfg['dest'] + nl)
    new_lines = lines[:si] + ptr.split(nl) + lines[ei:] if cfg.get('mid') else lines[:si] + ptr.split(nl)
    # drop blank line right before pointer for tidiness in mid case
    body = nl.join(new_lines) + (nl if text.endswith(nl) else '')
    data = body.encode('utf-8')
    if bom: data = b'\xef\xbb\xbf' + data
    open(p, 'wb').write(data)
    report[name] = dict(main=os.path.getsize(p), moved=len(block_text.encode('utf-8')), ref=os.path.getsize(dest),
                        within=12288 > os.path.getsize(p))
print(json.dumps(report, ensure_ascii=False, indent=1))