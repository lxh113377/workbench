import subprocess, tempfile, datetime
from pathlib import Path
now = datetime.datetime.now().astimezone(); today = datetime.date.today()
td = tempfile.mkdtemp(prefix='ufg_v110_')
def mk(advice, name):
    body = ('# t\n\n<!-- footer:begin session=v110 ts=%s -->\n'
            '[skill清单] 本轮调用=1 个 (A-memory-start)\n'
            '%s\n'
            '<!-- footer:end -->\n') % ((now - datetime.timedelta(seconds=20)).isoformat(), advice)
    p = Path(td) / (name + '.md'); p.write_bytes(body.encode('utf-8')); return str(p)
def run(p):
    r = subprocess.run(['python', r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', '--file', p, '--quiet'], capture_output=True)
    return r.returncode, (r.stdout + r.stderr).decode('utf-8', 'replace')

# A: 需裁决 但无裁决单路径 -> 须拦
rc, out = run(mk('[升级建议] skill=A-memory-start | 五问=缺失步骤 | 改法=试判据 | 对照=已取(实测) | 状态=需裁决(裁决单=)', 'a'))
print('A 需裁决无路径 -> rc', rc, '|', '需裁决未给裁决单路径' in out)
# B: 需裁决 路径不存在 -> 须拦
rc, out = run(mk('[升级建议] skill=A-memory-start | 五问=缺失步骤 | 改法=试判据 | 对照=已取(实测) | 状态=需裁决(裁决单=D:/nope/x.md)', 'b'))
print('B 裁决单不存在 -> rc', rc, '|', '裁决单不存在' in out)
# C: 需裁决 路径存在但缺三件套 -> 须拦
sheet = Path(td) / 'sheet_bad.md'; sheet.write_text('# 只有一句话', encoding='utf-8')
rc, out = run(mk('[升级建议] skill=A-memory-start | 五问=缺失步骤 | 改法=试判据 | 对照=已取(实测) | 状态=需裁决(裁决单=%s)' % sheet, 'c'))
print('C 缺三件套 -> rc', rc, '|', '三件套' in out)
# D: 需裁决 合规裁决单 -> 须放行
sheet2 = Path(td) / 'sheet_ok.md'; sheet2.write_text('# 裁决单\n选项A\n代价\n建议', encoding='utf-8')
rc, out = run(mk('[升级建议] skill=A-memory-start | 五问=缺失步骤 | 改法=试判据 | 对照=已取(实测) | 状态=需裁决(裁决单=%s)' % sheet2, 'd'))
print('D 合规需裁决 -> rc', rc, '| pass:', '[GATE:evolution-pass]' in out)
# E: 模糊挂起 待下轮 -> 须拦
rc, out = run(mk('[升级建议] skill=A-memory-start | 五问=缺失步骤 | 改法=试判据 | 对照=已取(实测) | 状态=未闭环(原因=待下轮再说)', 'e'))
print('E 模糊挂起 -> rc', rc, '|', '模糊挂起非法' in out)
# F: 待补（host 桩合法词）-> 放行
rc, out = run(mk('[升级建议] skill=A-memory-start | 五问=缺失步骤 | 改法=试判据 | 对照=已取(实测) | 状态=未闭环(原因=待补)', 'f'))
print('F 待补放行 -> rc', rc)