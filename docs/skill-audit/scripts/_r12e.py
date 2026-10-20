import re
QP = r'D:/workdesktop/docs/skill-audit/data/quality-proxy-baseline-2026-10-20.txt'
t = open(QP, encoding='utf-8').read()
lines = t.splitlines()
# 找汇总表与污染段
for i, l in enumerate(lines):
    if 'Top-1' in l and '方法' in l:
        print('=== 汇总表 ===')
        for x in lines[i:i+6]: print('  ', x[:120])
        break
for i, l in enumerate(lines):
    if '污染' in l:
        print('=== 污染段 ===')
        for x in lines[i:i+4]: print('  ', x[:130])
        break
for i, l in enumerate(lines):
    if '抽样区间' in l or 'Wilson' in l:
        print('=== 区间段 ===')
        for x in lines[i:i+6]: print('  ', x[:130])
        break