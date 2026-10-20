import json, datetime
p = r'D:/workdesktop/docs/skill-audit/data/sweep-report.json'
d = json.load(open(p, encoding='utf-8'))
unv = [r for r in d['rows'] if r['overall']=='UNVERIFIED']
jitter = [r['local_skill'] for r in unv if all(r[k]['state'] != 'UNVERIFIED' or k == 'leg_candidates' for k in r if k.startswith('leg_')) and '未取到读数' in r['leg_candidates']['note']]
d['rate_limit_pollution'] = {
    'detected_at': d['generated_at'],
    'rule': '05 补丁二：>=5 条候选腿「未取到读数」= 限流污染轮，结果不入口径，等 >=10 分钟重跑',
    'jitter_entries': jitter,
    'note': '本轮 5 条 UNVERIFIED 全部为候选腿读数失败（ungh.cc 限流），版本腿/漂移腿无一异常 ⇒ 数据本身无损，属通道抖动。基线仍以 16:15 轮（GREEN 61/RED 6/UNVERIFIED 0）与更早的干净轮为准。',
}
json.dump(d, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('rate_limit_pollution recorded, jitter entries:', len(jitter))
print(jitter)