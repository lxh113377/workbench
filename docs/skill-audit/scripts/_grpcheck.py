import re
for name, path in (('HEAD', r'D:/workdesktop/xtmp/_gate_head.py'), ('PATCHED', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py')):
    t = open(path, encoding='utf-8').read()
    m = re.search(r'RE_HIT = re\.compile\(\s*r"([^"]+)"', t)
    rx = re.compile(m.group(1))
    line = '[升级建议] skill=sites:sites-building | 五问=缺失步骤 | 改法=宿主存活校验 | 对照=已取(实测) | 状态=未闭环(原因=待补)'
    g = rx.match(line)
    print(name, '->', g.groups() if g else None)