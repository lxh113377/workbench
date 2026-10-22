(async () => {
  // pane_geometry.js —— 「子页被挡住」的几何判据读数（2026-10-20 老大反馈：
  // 「总览界面没问题，任务/产出/模板/设置子页面都被挡住了」）。
  //
  // 被挡住 = 子页首个内容块的上沿 落在导航区（顶栏+页签）下沿之上，也就是内容钻到了导航底下。
  // 典型根因是页签条被 flex 压成 0 高度（overlay 到内容上），肉眼看起来就是「内容被挡」。
  // 探针只取几何读数，判定在 assertions.mjs 的 pane 契约里（与其它契约同构）。
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };
  var rect = function (el) {
    if (!el) return null;
    var b = el.getBoundingClientRect();
    return { top: Math.round(b.top), bottom: Math.round(b.bottom), height: Math.round(b.height), width: Math.round(b.width) };
  };
  var errs = [];
  window.addEventListener("error", function (e) { errs.push(String(e.message)); });
  window.addEventListener("unhandledrejection", function (e) { errs.push(String((e.reason && e.reason.message) || e.reason)); });

  var out = [];
  var tabs = Array.prototype.slice.call(document.querySelectorAll(".tab"));
  for (var i = 0; i < tabs.length; i++) {
    var t = tabs[i];
    var name = t.getAttribute("data-tab");
    t.click();
    await sleep(900);
    var pane = document.getElementById("tab-" + name);
    var shell = document.querySelector(".topbar-shell");
    var tabbar = document.querySelector(".tabs");
    var paneR = rect(pane);
    var tabsR = rect(tabbar);
    var shellR = rect(shell);
    // 导航区下沿 = 页签条下沿与顶栏下沿的较大者（页签被压成 0 时也要能算对）
    var navBottom = Math.max(tabsR ? tabsR.bottom : 0, shellR ? shellR.bottom : 0);
    // 首个内容块：优先 .card，其次任意子元素
    var first = pane ? (pane.querySelector(".card") || pane.firstElementChild) : null;
    var firstR = rect(first);
    var paneTop = paneR ? paneR.top : null;
    var firstTop = firstR ? firstR.top : null;
    // 钻进导航区的像素数（>0 即被挡）；同时给出页签条自身高度（0 = 页签被压扁）
    var overlap = (firstTop !== null && navBottom !== null) ? Math.max(0, navBottom - firstTop) : null;
    var tabsHeight = tabsR ? tabsR.height : null;
    // 页签文字实际占位（页签被压成 0 时，tab 元素仍高 46 —— 这组读数用于区分「条被压扁」与「真被挡」）
    var tabFirstR = rect(document.querySelector(".tab"));
    out.push({
      tab: name, paneTop: paneTop, paneHeight: paneR ? paneR.height : null, firstBlockTop: firstTop,
      navBottom: navBottom, tabsHeight: tabsHeight, tabItemHeight: tabFirstR ? tabFirstR.height : null,
      overlap: overlap,
      paneDisplay: pane ? getComputedStyle(pane).display : null,
      paneTextLen: pane ? (pane.innerText || "").trim().length : 0,
      activeTabs: document.querySelectorAll(".tab.active").length,
      visiblePanes: document.querySelectorAll(".tabpane.active").length,
      // 内容可见像素：pane 顶到底边（视口内）的可见部分
      paneVisiblePx: paneR ? Math.max(0, Math.min(innerHeight, paneR.bottom) - Math.max(navBottom, paneR.top)) : 0
    });
  }
  var sc = document.scrollingElement;
  // 导航区占视口的比例（2026-10-20 实测：用户窗口 1040x613 物理 / DPR1.75 = 594x350 CSS，
  // 原导航吃掉 239px = 68%，子页只剩 99px 可见 ⇒ 体感「子页面被挡住」）。
  var tabsBar = document.querySelector(".tabs");
  var shellBar = document.querySelector(".topbar-shell");
  var tabsH2 = tabsBar ? Math.round(tabsBar.getBoundingClientRect().height) : 0;
  var shellH2 = shellBar ? Math.round(shellBar.getBoundingClientRect().height) : 0;
  var navH2 = Math.max(tabsH2, shellH2);
  return ascii({
    vp: [innerWidth, innerHeight], dpr: devicePixelRatio, panes: out,
    navHeight: navH2,
    navRatio: innerHeight > 0 ? Math.round(1000 * navH2 / innerHeight) / 10 : null,
    errors: errs.slice(0, 8),
    overflowX: Math.max(0, sc.scrollWidth - sc.clientWidth)
  });
})()