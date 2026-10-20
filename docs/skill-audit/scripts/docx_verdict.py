import json
p = r'D:/workdesktop/docs/skill-audit/data/skill-watchlist.json'
reg = json.load(open(p, encoding='utf-8'))
for e in reg['watchlist']:
    if e['local_skill'] == 'docx':
        e['notes'] = (e.get('notes') or '') + ' | 2026-10-20 裁决=保持本地(不替换) 依据=结构对账：本地 42 标题(含 Part A-F 全管线) 覆盖官方 docx 9 标题；官方全部脚本面(office/soffice/validate --auto-repair/accept_changes/comment/merge-runs)本地 15 脚本实存且更多(doctor/md_to_docx/preview/fill_template)；官方禁 pretty-print 与本地两阶段设计(unpack 美化→pack 压缩)不矛盾；4.19x 属完整非堆料'
json.dump(reg, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('docx verdict recorded')