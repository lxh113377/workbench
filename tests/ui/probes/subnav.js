(async () => {
  // subnav.js —— 管理面板 hash 深链 + 返回键收敛读数（子界面导航契约）。
  // 三态：① 带 #logs 直接打开 → ② 点 queue 页签 → ③ history.back()。
  // 判据在 tests/ui/assertions.mjs 的 subnav 契约（hash 与激活页签必须始终一致）。
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var activeTabName = function () {
    var t = document.querySelector('.tab.active');
    return t ? t.getAttribute('data-tab') : null;
  };
  var activePaneName = function () {
    var p = document.querySelector('.tabpane.active');
    return p ? String(p.getAttribute('id') || '').replace(/^tab-/, '') : null;
  };
  var sc = document.scrollingElement;

  // ① 深链：hash 必须在加载后仍是 #logs，且激活页签收敛到 logs（不是默认 overview）。
  var historyLengthOnLoad = history.length;
  var hashOnLoad = location.hash;
  var tabFromHashOnLoad = activeTabName();
  await sleep(400); // 等 app.js 的初始 activateTab + render 落定，避免读到默认 overview 的中间态
  var tabOnLoad = activeTabName();
  var paneOnLoad = activePaneName();
  var activeTabsOnLoad = document.querySelectorAll('.tab.active').length;
  var activePanesOnLoad = document.querySelectorAll('.tabpane.active').length;

  // ② 点 queue 页签：hash 应变为 #queue，激活页签与 pane 同步跟上。
  var q = document.querySelector('.tab[data-tab="queue"]');
  if (q) { q.click(); await sleep(700); }
  var hashAfterClick = location.hash;
  var tabAfterClick = activeTabName();
  var paneAfterClick = activePaneName();
  var activeTabsAfterClick = document.querySelectorAll('.tab.active').length;
  var activePanesAfterClick = document.querySelectorAll('.tabpane.active').length;

  // ③ history.back()：应回到 #logs，且激活页签跟着回 logs（hashchange 收敛，不允许 hash 回了页签没回）。
  history.back();
  await sleep(700);
  var historyLengthBeforeBack = history.length;
  var hashAfterBack = location.hash;
  var tabAfterBack = activeTabName();
  var paneAfterBack = activePaneName();
  var activeTabsAfterBack = document.querySelectorAll('.tab.active').length;
  var activePanesAfterBack = document.querySelectorAll('.tabpane.active').length;

  return ascii({
    historyLengthOnLoad: historyLengthOnLoad, historyLengthBeforeBack: historyLengthBeforeBack,
    hashOnLoad: hashOnLoad, tabFromHashOnLoad: tabFromHashOnLoad, tabOnLoad: tabOnLoad, paneOnLoad: paneOnLoad,
    activeTabsOnLoad: activeTabsOnLoad, activePanesOnLoad: activePanesOnLoad,
    hashAfterClick: hashAfterClick, tabAfterClick: tabAfterClick, paneAfterClick: paneAfterClick,
    activeTabsAfterClick: activeTabsAfterClick, activePanesAfterClick: activePanesAfterClick,
    hashAfterBack: hashAfterBack, tabAfterBack: tabAfterBack, paneAfterBack: paneAfterBack,
    activeTabsAfterBack: activeTabsAfterBack, activePanesAfterBack: activePanesAfterBack,
    overflowX: Math.max(0, sc.scrollWidth - sc.clientWidth)
  });
})()
