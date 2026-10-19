Here’s the requested **hello.py** script that prints “hello from workbench”. After creating the file, the verification step runs it with Python to ensure it exits cleanly (exit code 0).

<<<ARTIFACTS>>>
```
`manifest
{"files":[{"path":"hello.py","lang":"python"}],
 "verify":[{"cmd":"python hello.py","expectExit":0,"timeout":60}]}
```
`file:hello.py
print("hello from workbench")
```
<<<END>>>