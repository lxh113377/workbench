import os, shutil
QP = r'D:/workdesktop/docs/skill-audit/data/quality-proxy-baseline-2026-10-20.txt'
raw = open(QP, 'rb').read()
t = raw.decode('utf-16')   # 内容完好
# 备份原件（不删，留证）
bak = QP.replace('.txt', '.utf16-orig.txt')
if not os.path.isfile(bak): shutil.copy2(QP, bak)
# 转 UTF-8 无 BOM 写回（零内容丢失，只换编码）
open(QP, 'w', encoding='utf-8', newline='').write(t)
print('converted to utf-8:', os.path.getsize(QP), 'B | backup:', os.path.getsize(bak), 'B')
print('roundtrip check:', open(QP, encoding='utf-8').read() == t)
# 抽取关键读数供文档引用（UTF-8 后可读）
import re
for pat in ('Top-1', 'Top-3', 'n=', '污染', '区间'):
    for l in t.splitlines():
        if pat in l: print('  [%s] %s' % (pat, l.strip()[:110])); break