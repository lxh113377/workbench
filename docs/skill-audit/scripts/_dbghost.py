import importlib.util, sys, datetime, tempfile, os
from pathlib import Path
spec = importlib.util.spec_from_file_location('g', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py')
g = importlib.util.module_from_spec(spec); spec.loader.exec_module(g)
now = datetime.datetime.now().astimezone()
today = datetime.date.today()
td = tempfile.mkdtemp(prefix='ufg_dbg_')
g.EXTRA_CLOSED_ROOTS.append(td)
body = ('# t\n\n<!-- footer:begin session=dbg ts=%s -->\n'
        '[skill清单] 本轮调用=1 个 (A-memory-start)\n'
        '[升级建议] skill=%s | 五问=缺失步骤 | 改法=宿主存活校验 | 对照=已取(实测) | 状态=未闭环(原因=待补)\n'
        '<!-- footer:end -->\n')
for sk in ('sites:sites-building', 'A-memory-start'):
    p = Path(td) / ('h_%s.md' % sk.replace(':','_'))
    p.write_bytes((body % ((now - datetime.timedelta(seconds=30)).isoformat(), sk)).encode('utf-8'))
    lines = p.read_text(encoding='utf-8').splitlines()
    blk = g.split_last_block(lines)
    errs, stats = g.validate(blk[0], begin_line=blk[1]) if isinstance(blk, tuple) else g.validate(blk)
    print(sk, '-> errs:', errs)