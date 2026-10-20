import json, os, glob, re, datetime
BASE = 'D:/workdesktop/docs/skill-audit'
scripts = sorted(os.path.basename(p) for p in glob.glob(BASE + '/scripts/*.py'))
print('脚本总数:', len(scripts))
# 1) 临时诊断脚本（_rNN 前缀是我 17 轮的即兴工具，非长效机制件）
tmp = [s for s in scripts if re.match(r'^_r\d', s)]
print('临时诊断脚本(_rNN*):', len(tmp))
# 2) 哪些被其他脚本引用（cron/文档点名）= 长效机制件
alltext = ''
for f in glob.glob(BASE + '/**/*.py', recursive=True) + glob.glob(BASE + '/**/*.md', recursive=True):
    try: alltext += open(f, encoding='utf-8', errors='replace').read()
    except Exception: pass
core = [s for s in scripts if not s.startswith('_')]
unref = [s for s in core if alltext.count(s) <= 1]  # 只在自己文件名出现
print('核心脚本(非_前缀):', len(core), '| 其中无任何引用:', len(unref))
for s in unref: print('   UNREF:', s)
# 3) 数据目录的孤儿件
data = sorted(os.path.basename(p) for p in glob.glob(BASE + '/data/*'))
print()
print('数据文件数:', len(data))
orphan = [d for d in data if d.startswith('_')]
print('下划线临时数据件:', len(orphan), orphan[:12])