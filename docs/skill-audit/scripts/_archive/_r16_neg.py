import json, subprocess, tempfile, os
from pathlib import Path
# 负例测试：造一个无裁决的 RED，验证判据8真能拦
P = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(P, encoding='utf-8'))
# 备份 notes 后删掉一条已裁决的
orig = None
for e in reg['watchlist']:
    if e['local_skill'] == 'story':
        orig = e.get('notes'); e['notes'] = '(负例测试：裁决记录被临时移除)'
        break
tmp = tempfile.mkdtemp(prefix='leg0_neg_')
regp = os.path.join(tmp, 'reg.json')
json.dump(reg, open(regp, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
r = subprocess.run(['python', r'D:/workdesktop/docs/skill-audit/scripts/leg0_consistency_audit.py', '--registry', regp], capture_output=True)
out = (r.stdout + r.stderr).decode('utf-8', 'replace')
print('=== 负例（story 无裁决）===')
print(out.strip()[:300])
print('rc =', r.returncode, '| 判据8 拦住:', 'story 是 RED 但无裁决记录' in out)
# 恢复
for e in reg['watchlist']:
    if e['local_skill'] == 'story':
        e['notes'] = orig
json.dump(reg, open(P, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('=== 恢复后 ===')
r2 = subprocess.run(['python', r'D:/workdesktop/docs/skill-audit/scripts/leg0_consistency_audit.py'], capture_output=True)
print((r2.stdout + r2.stderr).decode('utf-8','replace').strip()[:120], '| rc =', r2.returncode)