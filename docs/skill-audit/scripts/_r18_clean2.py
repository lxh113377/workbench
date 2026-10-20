import os, glob, shutil
BASE = 'D:/workdesktop/docs/skill-audit'
DA = BASE + '/data'
DAR = DA + '/_archive'
os.makedirs(DAR, exist_ok=True)
moved = []
for p in glob.glob(DA + '/_*'):
    if os.path.isdir(p):
        continue   # 跳过 _archive 目录自身（自引用坑）
    n = os.path.basename(p)
    if n in ('_mini-reg.json', '_mini-out.json'):
        continue   # 这两个被 leg0/probe 当临时注册表用，留
    shutil.move(p, os.path.join(DAR, n)); moved.append(n)
print('archived data debris:', len(moved), moved[:6])
XT = 'D:/workdesktop/xtmp'
if os.path.isdir(XT):
    XA = XT + '/_archive_upstream'
    os.makedirs(XA, exist_ok=True)
    n = 0
    for p in glob.glob(XT + '/_upstream_*.md'):
        if os.path.isfile(p): shutil.move(p, os.path.join(XA, os.path.basename(p))); n += 1
    print('archived xtmp upstream snapshots:', n)
drest = sorted(os.path.basename(p) for p in glob.glob(DA + '/*') if os.path.isfile(p))
print('data/ 现存文件:', len(drest))
print('data/_archive 内:', len(os.listdir(DAR)))
print('scripts/_archive 内:', len(os.listdir(BASE + '/scripts/_archive')))
print('scripts/ 现存 .py:', len([p for p in glob.glob(BASE + '/scripts/*.py')]))