// icon-sets-sync web UI: the whole of its own JavaScript. Every page works
// without it; this only adds conveniences on top. It never builds HTML from
// strings: every value goes in through textContent.
(function () {
  "use strict";

  // Reload while background work the page is waiting on is running.
  var reload = document.body.getAttribute("data-reload-after");
  if (reload) {
    window.setTimeout(function () { window.location.reload(); }, Number(reload) * 1000);
  }

  // A form with data-confirm asks first. The server still requires its own
  // typed confirmation for a real sync; this is the first of the two.
  document.addEventListener("submit", function (ev) {
    var msg = ev.target.getAttribute && ev.target.getAttribute("data-confirm");
    if (msg && !window.confirm(msg)) {
      ev.preventDefault();
    }
  });

  // Log level filter: the CSS hides the levels below the chosen one.
  var log = document.getElementById("job-log");
  var filter = document.querySelector("[data-log-filter]");
  if (log && filter) {
    filter.addEventListener("change", function () { log.setAttribute("data-level", filter.value); });
  }
  var follow = document.getElementById("log-follow");
  function scrollLog() {
    if (log && (!follow || follow.checked)) { log.scrollTop = log.scrollHeight; }
  }
  scrollLog();

  var job = document.getElementById("job");
  if (!job || job.getAttribute("data-live") !== "1" || !window.EventSource) {
    return;
  }

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) { n.className = cls; }
    if (text !== undefined) { n.textContent = text; }
    return n;
  }
  function badgeClass(status) {
    switch (status) {
      case "succeeded": case "synced": case "up-to-date": case "finished": return "badge bg-green-lt";
      case "running": case "started": return "badge bg-blue-lt";
      case "cancelled": return "badge bg-secondary-lt";
      default: return "badge bg-red-lt";
    }
  }
  function clock(iso) {
    var d = new Date(iso);
    return isNaN(d) ? "" : d.toISOString().substring(11, 19);
  }

  var packs = document.getElementById("job-packs");
  var stages = document.getElementById("job-stages");
  var status = document.getElementById("job-status");

  function packItem(name) {
    var items = packs.querySelectorAll("[data-pack]");
    for (var i = 0; i < items.length; i++) {
      if (items[i].getAttribute("data-pack") === name) { return items[i]; }
    }
    var empty = packs.querySelector("[data-role=empty]");
    if (empty) { empty.remove(); }
    var li = el("li", "list-group-item");
    li.setAttribute("data-pack", name);
    var row = el("div", "d-flex justify-content-between");
    row.appendChild(el("code", "", name));
    var badge = el("span", "badge", "");
    badge.setAttribute("data-role", "status");
    row.appendChild(badge);
    li.appendChild(row);
    packs.appendChild(li);
    return li;
  }
  function setLine(parent, role, cls, text) {
    var n = parent.querySelector("[data-role=" + role + "]");
    if (!text) { if (n) { n.remove(); } return; }
    if (!n) { n = el("div", cls); n.setAttribute("data-role", role); parent.appendChild(n); }
    n.textContent = text;
  }

  var handlers = {
    "log": function (e) {
      var li = el("li", "log-line log-" + (e.level || "info"));
      var t = el("time", "", clock(e.time));
      t.setAttribute("datetime", e.time);
      li.appendChild(t);
      li.appendChild(document.createTextNode(" "));
      li.appendChild(el("span", "log-level", e.level || "info"));
      li.appendChild(document.createTextNode(" " + (e.msg || "")));
      if (e.attrs) {
        Object.keys(e.attrs).sort().forEach(function (k) {
          li.appendChild(document.createTextNode(" "));
          li.appendChild(el("span", "log-attr", k + "=" + e.attrs[k]));
        });
      }
      log.appendChild(li);
      scrollLog();
    },
    "stage": function (e) {
      var key = e.pipeline + "|" + e.stage;
      var item = null;
      var items = stages.querySelectorAll("[data-stage]");
      for (var i = 0; i < items.length; i++) {
        if (items[i].getAttribute("data-stage") === key) { item = items[i]; }
      }
      if (!item) {
        item = el("li", "list-group-item");
        item.setAttribute("data-stage", key);
        var b = el("span", "", "");
        b.setAttribute("data-role", "state");
        item.appendChild(b);
        item.appendChild(el("code", "", e.stage));
        item.appendChild(document.createTextNode(" "));
        item.appendChild(el("span", "text-secondary small", e.pipeline));
        stages.appendChild(item);
      }
      var state = item.querySelector("[data-role=state]");
      state.className = badgeClass(e.state) + " me-2";
      state.textContent = e.state;
      setLine(item, "error", "text-danger small", e.error || "");
    },
    "pack-start": function (e) {
      var li = packItem(e.pack);
      var b = li.querySelector("[data-role=status]");
      b.className = badgeClass("running");
      b.textContent = "running";
      setLine(li, "detail", "text-secondary small", e.msg || "");
      status.textContent = "Running: " + e.pack;
    },
    "pack-done": function (e) {
      var li = packItem(e.pack);
      var b = li.querySelector("[data-role=status]");
      b.className = badgeClass(e.status);
      b.textContent = e.status;
      if (e.msg) { setLine(li, "detail", "text-secondary small", e.msg); }
      setLine(li, "error", "text-danger small", e.error || "");
      status.textContent = e.pack + ": " + e.status;
    },
    "done": function (e) {
      status.textContent = "Run " + e.status + (e.error ? ": " + e.error : "") + ". Reloading the final summary.";
      source.close();
      window.setTimeout(function () { window.location.reload(); }, 1500);
    }
  };

  var url = job.getAttribute("data-events-url") + "?after=" + encodeURIComponent(job.getAttribute("data-last-id") || "0");
  var source = new EventSource(url);
  Object.keys(handlers).forEach(function (type) {
    source.addEventListener(type, function (msg) {
      var data;
      try { data = JSON.parse(msg.data); } catch (err) { return; }
      handlers[type](data);
    });
  });
})();
