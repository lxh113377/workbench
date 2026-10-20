import urllib.request, re, html
UA='Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36'
req=urllib.request.Request('https://github.com/anthropics/skills',headers={'User-Agent':UA})
h=urllib.request.urlopen(req,timeout=25).read().decode('utf-8','replace')
i=h.find('repo-stars-counter')
print('idx',i)
print(h[i-300:i+500] if i>0 else 'NOSTARS')
print('---JSON---')
j=h.find('stargazers_count')
print(h[j-200:j+200] if j>0 else 'no stargazers_count')