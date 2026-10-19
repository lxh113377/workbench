"""端到端证明：OFM aggregate 车道能否被真实 DSH headless 调用。

结论用途：为 Lead 在 executor.taskHome 的两行接线去风险 —— 证明
「池里加一个自填兼容端点 → DSH 真的把请求打到那个端点」这条链在
OFM 插件侧是通的（不是我们一侧的假设）。

全程零窗口：所有子进程用 DETACHED_PROCESS | CREATE_NO_WINDOW 派生，
stdout/stderr 落文件而非管道（上版实验用 .NET 管道读，Electron 子进程
继承写端导致 ReadToEnd 永不返回 —— 文件重定向绕开该坑）。
"""
import json
import os
import shutil
import subprocess
import sys
import time
import urllib.request

ROOT = os.environ.get("AGG_E2E_ROOT") or os.path.join(
    os.environ.get("TEMP", r"D:\workdesktop\runtime\tmp"), "wb-agg-e2e")
MOCK = r"D:\workdesktop\dist\mockofm.exe"
DSH_EXE = r"D:\deepseek desktop\DSH Desktop\DSH Desktop.exe"
CLI = r"D:\deepseek desktop\DSH Desktop\resources\app\lib\desktop-cli.js"
USER_PROFILES = r"C:\Users\37533\.dsh\profiles"
MOCK_PORT = int(os.environ.get("AGG_E2E_PORT", "18434"))
MOCK_KEY = "sk-user-key-XYZW"
PROVIDER_ID = "open_router"
MODEL_ID = "openrouter/free"
COMPOSITE = PROVIDER_ID + "/" + MODEL_ID

DETACHED = 0x00000008
NEWGROUP = 0x00000200
NOWINDOW = 0x08000000
FLAGS = DETACHED | NEWGROUP | NOWINDOW


def sh(*args, **kw):
    return subprocess.run(list(args), capture_output=True, text=True, **kw)


def http_json(url, data=None, timeout=8):
    req = urllib.request.Request(url, data=data)
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.loads(r.read().decode("utf-8"))


def main():
    if os.path.exists(ROOT):
        shutil.rmtree(ROOT, ignore_errors=True)
    home = os.path.join(ROOT, "home")
    ofm = os.path.join(home, "our-free-model")
    ws = os.path.join(ROOT, "ws")
    os.makedirs(ofm, exist_ok=True)
    os.makedirs(ws, exist_ok=True)

    # 1) mock OpenAI 兼容端点 = 用户自填的第三方接口
    mock_log = open(os.path.join(ROOT, "mock.out"), "wb")
    mock = subprocess.Popen([MOCK, "-addr", "127.0.0.1:%d" % MOCK_PORT, "-key", MOCK_KEY],
                            stdout=mock_log, stderr=mock_log, creationflags=FLAGS)
    ready = False
    for _ in range(20):
        time.sleep(0.5)
        try:
            http_json("http://127.0.0.1:%d/healthz" % MOCK_PORT)
            ready = True
            break
        except Exception:
            pass
    print("MOCK_READY=%s" % ready)
    if not ready:
        mock.kill()
        return 1

    # 2) 隔离 DSH_HOME：profiles junction + aggregate 打开的 settings.json
    sh("cmd", "/c", "mklink", "/J", os.path.join(home, "profiles"), USER_PROFILES)
    junction_ok = os.path.isdir(os.path.join(home, "profiles", "web-desktop"))
    print("PROFILES_JUNCTION=%s" % junction_ok)
    settings = {
        "version": 1, "enabled": True,
        "exposeRegionModels": True, "hiddenModelIds": [],
        "hideFreeModels": True, "hideEacModels": True,   # 关键：关掉另两条车道，排除串道
        "probeIntervalMinutes": 240, "defaultMaxTokens": 4096,
        "streamRecovery": True,
        "tokenSaver": {"enabled": False, "rtk": False},
        "aggregate": {"enabled": True, "providers": {
            PROVIDER_ID: {"enabled": True, "apiKey": MOCK_KEY,
                          "baseUrl": "http://127.0.0.1:%d/v1" % MOCK_PORT,
                          "models": [MODEL_ID], "addedAt": 1}}},
        "combos": [], "notifyOs": False, "updateCheckHours": 0,
    }
    with open(os.path.join(ofm, "settings.json"), "w", encoding="utf-8") as f:
        json.dump(settings, f, ensure_ascii=False)
    print("SETTINGS_WRITTEN aggregate.enabled=True baseUrl->mock")

    # 3) overlay：钉住 aggregate 车道的复合 id（provider/model）
    overlay = os.path.join(ROOT, "overlay.yml")
    with open(overlay, "w", encoding="utf-8") as f:
        f.write("- id: agent-default-model\n"
                "  config:\n"
                "    provider: our-free-model\n"
                "    model: %s\n"
                "    reasoningEffort: light\n" % COMPOSITE)

    # 4) 真实 DSH headless（与 executor.run 完全同参数形态）
    prompt = os.path.join(ROOT, "prompt.txt")
    with open(prompt, "w", encoding="utf-8") as f:
        f.write("Say exactly the word PONG and nothing else.")
    out = open(os.path.join(ROOT, "dsh.out"), "wb")
    err = open(os.path.join(ROOT, "dsh.err"), "wb")
    env = dict(os.environ)
    env["DSH_HOME"] = home
    env["ELECTRON_RUN_AS_NODE"] = "1"
    env["DSH_PERMISSION_MODE"] = "danger-full-access"
    env["DSH_TELEMETRY_DISABLED"] = "1"
    with open(prompt, "rb") as stdin_f:
        proc = subprocess.Popen(
            [DSH_EXE, "--expose-internals", CLI, "--profile", "web-desktop",
             "--patch", overlay, "--json", "-"],
            cwd=ws, stdin=stdin_f, stdout=out, stderr=err, env=env,
            creationflags=FLAGS)
        deadline = time.time() + 180
        while proc.poll() is None and time.time() < deadline:
            time.sleep(2)
        timed_out = proc.poll() is None
        if timed_out:
            proc.kill()
            time.sleep(1)
    out.close(); err.close()
    print("DSH_EXIT=%s TIMEOUT=%s" % (proc.returncode, timed_out))

    # 5) 决定性判据：mock 有没有真收到 chat 请求
    try:
        state = http_json("http://127.0.0.1:%d/ctl/state" % MOCK_PORT)
    except Exception as exc:
        state = {"error": str(exc)}
    print("MOCK_STATE chatHits=%s modelsHits=%s" % (state.get("chatHits"), state.get("modelsHits")))

    with open(os.path.join(ROOT, "dsh.out"), "r", encoding="utf-8", errors="replace") as f:
        body = f.read()
    print("DSH_OUT_BYTES=%d" % len(body))
    hits = [ln for ln in body.splitlines() if any(k in ln for k in ("pong", "PONG", "error", "Error", "aggregate", "no adapter", "unknown provider"))]
    for ln in hits[:12]:
        print("  FRAME " + (ln[:260] + ("..." if len(ln) > 260 else "")))
    with open(os.path.join(ROOT, "dsh.err"), "r", encoding="utf-8", errors="replace") as f:
        ebody = f.read()
    for ln in [x for x in ebody.splitlines() if x.strip()][-6:]:
        print("  STDERR " + ln[:220])

    verdict = "PASS" if (state.get("chatHits") or 0) >= 1 else "NO_UPSTREAM_HIT"
    print("AGGREGATE_E2E=%s" % verdict)
    mock.kill()
    # 判据：mock 真收到 chat 请求（证明请求打到用户自填端点，而非静默改道免费车道）
    # 且 DSH 正常退出并产出 final 文本。任一不满足即失败退出，供 CI/脚本判定。
    ok = (state.get("chatHits") or 0) >= 1 and not timed_out and proc.returncode == 0 \
         and ('"final"' in body or '"text"' in body)
    print("RESULT:", "evidence complete" if ok else "evidence incomplete")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
