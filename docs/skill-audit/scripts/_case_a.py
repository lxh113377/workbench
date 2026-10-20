import subprocess, tempfile, datetime
from pathlib import Path
now = datetime.datetime.now().astimezone()
td = tempfile.mkdtemp(prefix='ufg_a_')
body = ('# t\n\n<!-- footer:begin session=v110 ts=%s -->\n'
        '[skill清单] 本轮调用=1 个 (A-memory-start)\n'
        '[升级建议] skill=A-memory-start | 五问=缺失步骤 | 改法=试判据 | 对照=已取(实测) | 状态=需裁决(裁决单=)\n'
        '<!-- footer:end -->\n') % (now - datetime.timedelta(seconds=20)).isoformat()
p = Path(td) / 'a.md'; p.write_bytes(body.encode('utf-8'))
r = subprocess.run(['python', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', '--file', str(p)], capture_output=True)
print((r.stdout + r.stderr).decode('utf-8', 'replace')[:700])