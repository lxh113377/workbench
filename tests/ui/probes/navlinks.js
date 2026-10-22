(async () => {
  // navlinks.js —— 子界面互通契约读数：把页面里所有 href 收集起来，交给断言层判「必需链接」是否齐。
  // 为什么不在探针里直接判：判据必须能被反证（见 docs/前端探针判据修复-2026-10-19.md 反证章节），
  // 探针只负责取原文读数，判定统一在 tests/ui/assertions.mjs。
  var ascii = function (o) { return JSON.stringify(o).replace(/[\u0080-\uffff]/g, function (c) { return "\\u" + c.charCodeAt(0).toString(16).padStart(4, "0"); }); };
  var links = [];
  var seen = {};
  var as = document.querySelectorAll('a[href]');
  for (var i = 0; i < as.length; i++) {
    var href = as[i].getAttribute('href') || '';
    var path = href;
    // 只留 path(+query+hash)，去掉 origin，便于前缀匹配与跨端口可比。
    try { var u = new URL(href, location.origin); path = u.pathname + u.search + u.hash; } catch (e) { /* 原样保留 */ }
    if (seen[href]) continue;
    seen[href] = true;
    links.push({ href: href, path: path, text: (as[i].textContent || '').trim().slice(0, 40), target: as[i].getAttribute('target') || '' });
  }
  var page = (location.pathname.indexOf('admin') >= 0) ? 'admin' : (location.pathname.indexOf('governance') >= 0 ? 'governance' : (location.pathname.indexOf('modelpool') >= 0 ? 'modelpool' : 'other'));
  // 环境自证：页面到底是不是被测实例的子界面（host 非本机 / 没有 .tab 都说明探针落错了页，
  // 例如隔离实例到期后 Edge 落回本地 DNS 错误页）。这是「没测成」，不是页面缺链接。
  var hostnameOnPage = location.hostname;
  var landedOnTarget = (hostnameOnPage === '127.0.0.1' || hostnameOnPage === 'localhost');
  var hasTopbar = !!document.querySelector('.topbar');
  var tabCount = document.querySelectorAll('.tab').length;
  var linkCountRaw = as.length;
  var adminNeeds = ['/governance?token=', '/modelpool?token=', '/?token='];
  var govNeeds = ['/admin?token=', '/modelpool?token=', '/?token='];
  var paths = links.map(function (l) { return l.path; });
  var has = function (need) { for (var k = 0; k < paths.length; k++) { if (paths[k].indexOf(need) === 0) return true; } return false; };
  var missingOnAdmin = adminNeeds.filter(function (n) { return !has(n); });
  var missingOnGovernance = govNeeds.filter(function (n) { return !has(n); });
  var sc = document.scrollingElement;
  // missingRelevant = 「本页面该有的必需链接」的缺项。admin 页天然不会链到 /admin 自己，
  // 所以 missingOnGovernance 在 admin 页上恒含 "/admin?token="，供对照但不当判据；
  // 判据只认与当前页面匹配的那份（避免「按 goveernance 清单判 admin 页」的语义错位）。
  var missingRelevant = (page === 'governance') ? missingOnGovernance : (page === 'admin' ? missingOnAdmin : []);
  return ascii({ page: page, links: links, paths: paths,
    hostnameOnPage: hostnameOnPage, landedOnTarget: landedOnTarget, hasTopbar: hasTopbar, tabCount: tabCount, linkCountRaw: linkCountRaw,
    requiredAdmin: adminNeeds, requiredGovernance: govNeeds,
    missingOnAdmin: missingOnAdmin, missingOnGovernance: missingOnGovernance, missingRelevant: missingRelevant,
    overflowX: Math.max(0, sc.scrollWidth - sc.clientWidth) });
})()
