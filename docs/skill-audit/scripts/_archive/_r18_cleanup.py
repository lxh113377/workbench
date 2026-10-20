import os, re, glob, shutil, json
BASE = 'D:/workdesktop/docs/skill-audit'
SC = BASE + '/scripts'
AR = SC + '/_archive'
os.makedirs(AR, exist_ok=True)
# 1) 归档 _rNN* 临时诊断脚本（本会话即兴工具，结果已入数据件/提交信息，无信息损失）
moved = []
for p in glob.glob(SC + '/_r*.py'):
    n = os.path.basename(p)
    if re.match(r'^_r\d', n):
        shutil.move(p, os.path.join(AR, n)); moved.append(n)
print('archived temp scripts:', len(moved))
# 2) 归档数据目录的下划线碎片
DA = BASE + '/data'
DAR = DA + '/_archive'
os.makedirs(DAR, exist_ok=True)
moved_d = []
for p in glob.glob(DA + '/_*'):
    n = os.path.basename(p)
    if n in ('_mini-reg.json', '_mini-out.json'):  # 这两个被 leg0/probe 当临时注册表用，留
        continue
    shutil.move(p, os.path.join(DAR, n)); moved_d.append(n)
print('archived data debris:', len(moved_d))
# 3) 归档 xtmp 的上游快照（内容已在 references 吸收卷里）
XT = 'D:/workdesktop/xtmp'
if os.path.isdir(XT):
    XA = XT + '/_archive_upstream'
    os.makedirs(XA, exist_ok=True)
    n = 0
    for p in glob.glob(XT + '/_upstream_*.md'):
        shutil.move(p, os.path.join(XA, os.path.basename(p))); n += 1
    print('archived xtmp upstream snapshots:', n)
# 4) 剩余清单
rest = sorted(os.path.basename(p) for p in glob.glob(SC + '/*.py'))
print('scripts/ 现存 .py:', len(rest))
drest = sorted(os.path.basename(p) for p in glob.glob(DA + '/*'))
print('data/ 现存文件:', len(drest))