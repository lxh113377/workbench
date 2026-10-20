t = open(r'D:/workdesktop/xtmp/_gate_head.py', encoding='utf-8').read()
import re
for m in re.finditer(r'^.*V1\.9.*$', t, re.M):
    print(repr(m.group(0)[:160]))
print('---- first 12 lines ----')
for i, l in enumerate(t.splitlines()[:12]):
    print(i+1, repr(l[:150]))