import json, time, urllib.request
UA = 'Mozilla/5.0'
UNGH = 'https://ungh.cc/repos/{}'
p = r'D:/workdesktop/docs/skill-audit/data/score-matrix.json'
m = json.load(open(p, encoding='utf-8'))
def facts(repo):
    try:
        req = urllib.request.Request(UNGH.format(repo), headers={'User-Agent': UA})
        d = json.loads(urllib.request.urlopen(req, timeout=20).read().decode('utf-8','replace'))
        r = d.get('repo') or {}
        return r.get('stars'), r.get('pushedAt'), bool(r.get('archived')), (r.get('license') or '')
    except Exception as e:
        return None, None, None, 'ERR:' + repr(e)[:40]
fixed = 0
for s in m['skills']:
    for c in s.get('candidates', []):
        if c.get('stars') is None and c.get('repo'):
            st, pu, ar, lic = facts(c['repo'])
            if st is not None:
                c['stars'] = st
                if pu: c['last_activity'] = str(pu)[:10]
                if ar: c['archived'] = True
                if lic and not str(lic).startswith('ERR') and not c.get('license'): c['license'] = lic
                fixed += 1
                print('OK  %-28s %-34s stars=%s' % (s['local_skill'], c['repo'], st))
            else:
                c['stars_note'] = str(lic)[:60]
                print('ERR %-28s %-34s %s' % (s['local_skill'], c['repo'], str(lic)[:50]))
            time.sleep(0.5)
m['generated_at'] = json.load(open(r'D:/workdesktop/docs/skill-audit/data/score-matrix.json', encoding='utf-8'))['generated_at']
m['activity_refreshed_at'] = __import__('datetime').datetime.now().isoformat(timespec='seconds')
json.dump(m, open(p, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
print('fixed:', fixed)