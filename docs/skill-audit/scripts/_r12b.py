import os
QP = r'D:/workdesktop/docs/skill-audit/data/quality-proxy-baseline-2026-10-20.txt'
raw = open(QP, 'rb').read()
print('first 4 bytes:', raw[:4].hex(), '| BOM utf16le:', raw[:2] == bytes([0xFF, 0xFE]))
# 用 utf-16 解码看是否可读
try:
    t = raw.decode('utf-16')
    print('utf-16 decode OK, len:', len(t))
    for l in t.splitlines()[:14]: print('  ', l[:110])
except Exception as e:
    print('utf-16 decode ERR', repr(e)[:80])
    t = raw.decode('utf-8', errors='replace')
    for l in t.splitlines()[:6]: print('  utf8-replace:', l[:100])