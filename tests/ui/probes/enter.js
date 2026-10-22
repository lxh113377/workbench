(async () => {
  // enter.js —— 首页入口的端到端读数：真实点击唯一按钮，验证它把人送进主功能子界面。
  // 判据在 tests/ui/assertions.mjs 的 enter 契约；hrefAfter 由 cdp_probe 在点击后补采
  // （导航会销毁本次 eval 的上下文，所以这里只负责「等可点 → 点击 → 立刻返回」）。
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var btn = document.getElementById("startBtn");
  if (!btn) { return ascii({ error: "no-startBtn" }); }
  var snap = function () { return { state: btn.getAttribute("data-state"), label: (btn.querySelector(".btn-label") || {}).textContent, disabled: !!btn.disabled, href: location.href }; };
  var before = snap();
  // 等按钮进入可点态（idle/running/paused 任一）：开屏自动启动的 busy 窗口最长实测 ~2s，
  // 这里给 8s 上限；超时就照点不误 —— busy 态现在也必须可点（lock:false 契约）。
  var waited = 0;
  while (waited < 8000 && before.disabled) { await sleep(200); waited += 200; before.state = btn.getAttribute("data-state"); before.disabled = !!btn.disabled; }
  btn.click();
  var after = { state: btn.getAttribute("data-state"), label: (btn.querySelector(".btn-label") || {}).textContent, href: location.href };
  return ascii({ clicked: true, before: before, afterClick: after, waitedMs: waited, hasToken: !!window.__WB_TOKEN__ });
})()