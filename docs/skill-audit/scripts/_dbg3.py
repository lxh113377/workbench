import subprocess, tempfile, datetime, os
from pathlib import Path
now = datetime.datetime.now().astimezone()
td = tempfile.mkdtemp(prefix='ufg_dbg2_')
hit = ('# t\n\n<!-- footer:begin session=dbg ts=%s -->\n'
       '[skill清单] 本轮调用=1 个 (A-memory-start)\n'
       '[升级建议] skill=%s | 五问=缺失步骤 | 改法=宿主存活校验 | 对照=已取(实测) | 状态=未闭环(原因=待补)\n'
       '<!-- footer:end -->\n')
for sk in ('sites:sites-building', 'A-memory-start'):
    p = Path(td) / ('h_%s.md' % sk.replace(':','_').replace('-','_'))
    p.write_bytes((hit % ((now - datetime.timedelta(seconds=30)).isoformat(), sk)).encode('utf-8'))
    r = subprocess.run(['python', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', '--file', str(p)], capture_output=True)
    out = (r.stdout + r.stderr).decode('utf-8', 'replace')
    print('===', sk, 'rc=', r.returncode, '===')
    print(out[:600])