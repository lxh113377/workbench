import subprocess
raw = subprocess.run(['git','-C','D:\\global_skills','show','HEAD:A-memory-start/references/upgrade_footer_gate.py'], capture_output=True).stdout
t = raw.decode('utf-8', errors='replace')
open(r'D:/workdesktop/xtmp/_gate_head.py','w',encoding='utf-8').write(t)
# find version-note area and state block in HEAD base
i = t.find('新增校验：命中行')
print('--- header note area ---')
print(t[i:i+120])
j = t.find('def main(')
print('--- main at', j, '---')
k = t.find('if state == "未闭环" and not val:')
print('--- state block at', k, '---')
print(t[k-60:k+120])
m = t.rfind('def selftest(')
print('--- selftest at', m, '---')
print('main call inside selftest:', 'rc = main(argv_)' in t)