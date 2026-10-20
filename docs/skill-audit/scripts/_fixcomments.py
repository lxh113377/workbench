import re
p = r'D:/workdesktop/docs/skill-audit/scripts/build_watchlist.py'
lines = open(p, encoding='utf-8').read().splitlines()
n = 0
for i, l in enumerate(lines):
    s = l.strip()
    if s.startswith('// '):
        lines[i] = l.replace('//', '#', 1)
        n += 1
open(p, 'w', encoding='utf-8').write(chr(10).join(lines) + chr(10))
print('comment lines fixed:', n)