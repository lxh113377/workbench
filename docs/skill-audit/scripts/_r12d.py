import os
QP = r'D:/workdesktop/docs/skill-audit/data/quality-proxy-baseline-2026-10-20.txt'
orig = open(QP.replace('.txt', '.utf16-orig.txt'), 'rb').read().decode('utf-16')
new = open(QP, encoding='utf-8').read()
print('orig len:', len(orig), '| new len:', len(new))
# 逐字符找第一个差异
for i, (a, b) in enumerate(zip(orig, new)):
    if a != b:
        print('first diff at', i, repr(orig[max(0,i-30):i+30]), 'VS', repr(new[max(0,i-30):i+30]))
        break
else:
    print('prefix identical up to', min(len(orig), len(new)))
print('orig has CRLF:', orig.count(chr(13)+chr(10)), '| new has CRLF:', new.count(chr(13)+chr(10)))
print('orig LF only:', orig.count(chr(10)) - orig.count(chr(13)+chr(10)), '| new LF only:', new.count(chr(10)) - new.count(chr(13)+chr(10)))
norm_o = orig.replace(chr(13)+chr(10), chr(10))
norm_n = new.replace(chr(13)+chr(10), chr(10))
print('after newline-normalize identical:', norm_o == norm_n)