import json, os, datetime
QP = r'D:/workdesktop/docs/skill-audit/data/quality-proxy-baseline-2026-10-20.txt'
RT = r'C:/Users/37533/Desktop/workspace/焚诀/eval/route_trace.jsonl'
print('=== hitrate 基线冻结件 ===')
if os.path.isfile(QP):
    t = open(QP, encoding='utf-8', errors='replace').read()
    print('size', len(t.encode()), '| head:');
    for l in t.splitlines()[:12]: print('  ', l[:100])
    # 找污染剔除数与 n=
    import re
    for pat in ('污染', 'n=', 'Top-1', '区间'):
        m = [l for l in t.splitlines() if pat in l][:2]
        for x in m: print('   [%s] %s' % (pat, x[:110]))
print()
print('=== 当前 route_trace ===')
if os.path.isfile(RT):
    lines = open(RT, encoding='utf-8', errors='replace').read().splitlines()
    print('lines:', len(lines), '| bytes:', os.path.getsize(RT), '| mtime:', datetime.datetime.fromtimestamp(os.path.getsize(RT)).strftime('%m-%d %H:%M'))
    print('first:', lines[0][:120] if lines else 'EMPTY')
    print('last :', lines[-1][:120] if lines else 'EMPTY')
else:
    print('route_trace.jsonl 不存在')