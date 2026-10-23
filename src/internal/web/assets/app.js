// icon-sets-sync web UI: the whole of its own JavaScript. Every page works
// without it; this only adds conveniences on top.
(function () {
  "use strict";

  // Reload while background work the page is waiting on is running.
  var reload = document.body.getAttribute("data-reload-after");
  if (reload) {
    window.setTimeout(function () { window.location.reload(); }, Number(reload) * 1000);
  }
})();
