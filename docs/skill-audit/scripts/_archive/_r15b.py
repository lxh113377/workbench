import re
cur = open('D:/workdesktop/xtmp/_rehearse_hit.txt', encoding='utf-8').read()
base = open(r'D:/workdesktop/docs/skill-audit/data/quality-proxy-baseline-2026-10-20.txt', encoding='utf-8').read()
def grab(t, method):
    for l in t.splitlines():
        if l.strip().startswith(method):
            return l.strip()
    return None
print('%-12s %-58s' % ('方法', '当前（轮15 预演）'))
for m in ('关键词', 'TF-IDF', '混合路由'):
    print('%-12s %s' % (m, grab(cur, m)))
print()
print('%-12s %-58s' % ('方法', '冻结基线（2026-10-20）'))
for m in ('关键词', 'TF-IDF', '混合路由'):
    print('%-12s %s' % (m, grab(base, m)))
print()
# 污染数对比
for name, t in (('当前', cur), ('基线', base)):
    c = [l.strip() for l in t.splitlines() if 'contaminated' in l]
    print('%s 污染剔除: %s' % (name, c[:3]))