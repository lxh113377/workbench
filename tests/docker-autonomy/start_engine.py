import json,re,urllib.request,sys
def req(method,url,token=None,body=None):
    data=None if body is None else json.dumps(body).encode()
    r=urllib.request.Request(url,data=data,method=method)
    r.add_header("Content-Type","application/json")
    if token: r.add_header("X-WB-Token",token)
    with urllib.request.urlopen(r,timeout=20) as resp:
        return resp.read().decode("utf-8","replace")
url=open("/tmp/runtime/panel.url").read().strip()
html=req("GET",url)
token=re.search(r'window\.__WB_TOKEN__ = "([^"]+)"',html).group(1)
open("/tmp/token","w").write(token)
print("URL="+url)
print("TOKEN_LEN="+str(len(token)))
print(req("POST",url+"api/engine/start",token,{}))
