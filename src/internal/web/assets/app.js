// icon-sets-sync web UI: the whole of its own JavaScript. Every page works
// without it; this only adds conveniences on top. It never builds HTML from
// strings: every value goes in through textContent.
(function () {
  "use strict";

  // Reload while background work the page is waiting on is running.
  // The reload waits while a dialog or side panel is open or a field has
  // focus: reloading under them threw away what was being chosen (U11).
  var reload = document.body.getAttribute("data-reload-after");
  function busy() {
    var a = document.activeElement;
    return document.querySelector(".modal.show, .offcanvas.show, .offcanvas.showing") ||
      (a && /^(INPUT|SELECT|TEXTAREA)$/.test(a.tagName));
  }
  function reloadWhenIdle() {
    if (busy()) { window.setTimeout(reloadWhenIdle, 1000); return; }
    window.location.reload();
  }
  if (reload) {
    window.setTimeout(reloadWhenIdle, Number(reload) * 1000);
  }

  var tb = window.tabler || {};

  // A form with data-confirm asks first, in Tabler's danger modal
  // (#confirm-modal in the layout): data-confirm is the body text,
  // data-confirm-title and data-confirm-button the heading and the button.
  // The server still requires its own typed confirmation for a real sync;
  // this is the first of the two. Without the modal script it falls back to
  // the browser's own confirm.
  var modalEl = document.getElementById("confirm-modal");
  var pending = null;
  document.addEventListener("submit", function (ev) {
    var form = ev.target;
    var msg = form.getAttribute && form.getAttribute("data-confirm");
    if (!msg || form.getAttribute("data-confirmed") === "1" ||
        (ev.submitter && ev.submitter.hasAttribute("data-confirm-skip"))) {
      return;
    }
    ev.preventDefault();
    if (!modalEl || !tb.Modal) {
      if (window.confirm(msg)) { form.setAttribute("data-confirmed", "1"); form.requestSubmit(ev.submitter || undefined); }
      return;
    }
    pending = { form: form, submitter: ev.submitter || null };
    document.getElementById("confirm-modal-title").textContent = form.getAttribute("data-confirm-title") || "Are you sure?";
    document.getElementById("confirm-modal-body").textContent = msg;
    document.getElementById("confirm-modal-ok").textContent = form.getAttribute("data-confirm-button") || "Confirm";
    tb.Modal.getOrCreateInstance(modalEl).show();
  });
  if (modalEl) {
    document.getElementById("confirm-modal-ok").addEventListener("click", function () {
      if (!pending) { return; }
      var p = pending;
      pending = null;
      tb.Modal.getOrCreateInstance(modalEl).hide();
      p.form.setAttribute("data-confirmed", "1");
      if (p.submitter) { p.form.requestSubmit(p.submitter); } else { p.form.requestSubmit(); }
    });
    modalEl.addEventListener("hidden.bs.modal", function () { pending = null; });
  }

  // Tooltips and popovers are opt-in in Bootstrap; Tabler's own pages
  // initialise every one on load, and so does this.
  if (tb.Tooltip) {
    document.querySelectorAll("[data-bs-toggle=tooltip]").forEach(function (n) { tb.Tooltip.getOrCreateInstance(n); });
  }
  if (tb.Popover) {
    document.querySelectorAll("[data-bs-toggle=popover]").forEach(function (n) { tb.Popover.getOrCreateInstance(n); });
  }

  // The dashboard's per-pack sync panel: one offcanvas, pointed at the pack
  // whose row opened it. Without the script the row's button is a plain link
  // to the pack page, which has the same form.
  var packPanel = document.getElementById("pack-sync-panel");
  if (packPanel) {
    packPanel.addEventListener("show.bs.offcanvas", function (ev) {
      var slug = ev.relatedTarget && ev.relatedTarget.getAttribute("data-pack");
      if (!slug) { return; }
      var base = "/packs/" + encodeURIComponent(slug);
      packPanel.querySelector("[data-role=pack-sync-form]").setAttribute("action", base + "/sync");
      packPanel.querySelector("[data-role=pack-page]").setAttribute("href", base);
      packPanel.querySelector("[data-role=pack-name]").textContent = slug;
    });
  }

  // Copy buttons: data-copy-target names the element whose text is copied.
  document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-copy-target]");
    if (!btn) { return; }
    var src = document.getElementById(btn.getAttribute("data-copy-target"));
    if (!src || !navigator.clipboard) { return; }
    navigator.clipboard.writeText(src.textContent).then(function () {
      var label = btn.querySelector("[data-role=copy-label]");
      if (!label) { return; }
      var was = label.textContent;
      label.textContent = "Copied";
      window.setTimeout(function () { label.textContent = was; }, 1500);
    });
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

  // Config editor: keep the line numbers in step, and validate as you type
  // (debounced) through the same endpoint the Validate button posts to.
  var editor = document.getElementById("editor-text");
  if (editor) {
    var gutter = document.getElementById("editor-gutter");
    var form = document.getElementById("editor-form");
    var save = document.getElementById("editor-save");
    var findings = document.getElementById("editor-findings-list");
    var title = document.getElementById("findings-title");
    var timer = null;
    var csrf = document.querySelector("meta[name=csrf-token]").getAttribute("content");

    var syncGutter = function () {
      var n = editor.value.split("\n").length;
      if (editor.value.slice(-1) === "\n") { n--; }
      n = Math.max(n, 1);
      while (gutter.children.length < n) { gutter.appendChild(document.createElement("li")); }
      while (gutter.children.length > n) { gutter.removeChild(gutter.lastChild); }
      editor.style.height = "auto";
      editor.style.height = editor.scrollHeight + "px";
    };
    var show = function (res) {
      while (findings.firstChild) { findings.removeChild(findings.firstChild); }
      (res.findings || []).forEach(function (f) {
        var li = document.createElement("li");
        li.className = "finding finding-" + f.level;
        li.textContent = (f.level === "error" ? "✗ " : f.level === "warn" ? "! " : "✓ ") + f.text;
        findings.appendChild(li);
      });
      title.textContent = res.ok ? "Valid" : res.problems + " problem(s)";
      if (save) { save.disabled = !res.ok; }
    };
    var validate = function () {
      var body = new URLSearchParams();
      body.set("content", editor.value);
      body.set("csrf", csrf);
      fetch(form.getAttribute("data-validate-url"), {
        method: "POST",
        headers: { "Accept": "application/json", "X-CSRF-Token": csrf, "Content-Type": "application/x-www-form-urlencoded" },
        body: body.toString(),
        credentials: "same-origin"
      }).then(function (r) { return r.json(); }).then(show).catch(function () {});
    };
    editor.addEventListener("input", function () {
      syncGutter();
      if (save) { save.disabled = true; }
      window.clearTimeout(timer);
      timer = window.setTimeout(validate, 400);
    });
    syncGutter();
  }

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
      case "succeeded": case "synced": case "up-to-date": case "finished": return "badge badge-ok";
      case "running": case "started": return "badge badge-info";
      case "cancelled": return "badge badge-muted";
      default: return "badge badge-bad";
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
      setLine(item, "error", "text-danger small text-break", e.error || "");
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
      setLine(li, "error", "text-danger small text-break", e.error || "");
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
