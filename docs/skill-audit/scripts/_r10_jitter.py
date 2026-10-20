import json
p = r'D:/workdesktop/docs/skill-audit/data/sweep-report.json'
d = json.load(open(p, encoding='utf-8'))
unv = [r for r in d['rows'] if r['overall']=='UNVERIFIED']
jitter = [r['local_skill'] for r in unv if '未取到读数' in (r.get('leg_candidates') or {}).get('note','')]
# 轮10 修正：污染标记须按当轮实况重判（贴上去就要能撕下来），不是永久标记
d.pop('rate_limit_pollution', None)
if len(jitter) >= 5:
    d['rate_limit_pollution'] = {
        'detected_at': d['generated_at'],
        'rule': '05 补丁二：>=5 条候选腿「未取到读数」= 限流污染轮，结果不入口径',
        'jitter_entries': jitter,
        'note': '本轮 %d 条候选腿读数失败（ungh.cc 限流），数据本身无损。' % len(jitter),
    }
else:
    d['channel_jitter'] = {
        'detected_at': d['generated_at'],
        'jitter_entries': jitter,
        'note': '本轮 %d 条候选腿读数失败（<5 未达污染线）；属通道抖动，重跑可恢复，不算数据问题。' % len(jitter),
    }
json.dump(d, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('jitter entries:', len(jitter), jitter)
print('pollution flag now:', 'rate_limit_pollution' in d, '| channel_jitter:', 'channel_jitter' in d)