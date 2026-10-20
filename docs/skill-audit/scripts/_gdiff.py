import difflib
a = open(r'D:/workdesktop/xtmp/_gate_head.py', encoding='utf-8').read().splitlines()
b = open(r'D:/global_skills/A-memory-start/references/upgrade_footer_gate.py', encoding='utf-8').read().splitlines()
d = list(difflib.unified_diff(a, b, 'HEAD', 'patched', lineterm='', n=1))
print('diff lines:', len(d))
for l in d[:60]:
    print(l[:150])