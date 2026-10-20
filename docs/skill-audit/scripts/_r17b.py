import re
t = open(r'D:/workdesktop/docs/skill-audit/06-验收报告.md', encoding='utf-8').read()
for m in re.finditer(r'.{40}182.{40}', t):
    print(repr(m.group(0)))
print('---')
for m in re.finditer(r'.{30}183.{30}', t):
    print(repr(m.group(0)))