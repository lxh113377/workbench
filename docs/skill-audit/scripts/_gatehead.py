import subprocess, io
raw = subprocess.run(['git','-C','D:\\global_skills','show','HEAD:A-memory-start/references/upgrade_footer_gate.py'], capture_output=True).stdout
t = raw.decode('utf-8', errors='replace')
open(r'D:/workdesktop/xtmp/_gate_head.py','w',encoding='utf-8').write(t)
print('len', len(t))
i = t.find('RE_HIT = re.compile')
print('--- RE_HIT ---'); print(t[i:i+230])
j = t.find('if state == "未闭环" and not val:')
print('--- state block ---'); print(t[j-260:j+300])
k = t.find('def main(')
print('--- main ---'); print(t[k:k+1100] if k>=0 else 'NOT FOUND')