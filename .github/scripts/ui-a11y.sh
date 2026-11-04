#!/usr/bin/env bash
# Serves the web UI against the a11y fixture and runs axe over every page, in
# the light and the dark theme. Fails on any serious or critical violation, and
# on scanning fewer pages than it meant to -- a scan that loaded nothing would
# otherwise report a clean site.
#
#   AXE_VERSION   the pinned @axe-core/cli version (required)
#   BIN           the icon-sets-sync binary to serve (required)
#   CHROMEDRIVER  chromedriver matching the installed Chrome (required)
set -euo pipefail

: "${AXE_VERSION:?}" "${BIN:?}" "${CHROMEDRIVER:?}"
work=${RUNNER_TEMP:-$(mktemp -d)}/ui-a11y
fixture=src/internal/web/testdata/a11y
rm -rf "$work"
mkdir -p "$work/state"

# The pack tree is a git repository in real use; the dashboard reads its branch.
cp -R "$fixture/icon-sets-a11y" "$work/icon-sets-a11y"
git -C "$work/icon-sets-a11y" init -q -b main
git -C "$work/icon-sets-a11y" -c user.name=ci -c user.email=ci@example.invalid add -A
git -C "$work/icon-sets-a11y" -c user.name=ci -c user.email=ci@example.invalid commit -q -m fixture

# The fixture's upstream: version_check_url points here. Served by the
# repository's own src/internal/tools/staticserve, built like the binary.
go -C src build -o "$work/staticserve" ./internal/tools/staticserve
"$work/staticserve" -addr 127.0.0.1:8765 -dir "$fixture/upstream" >"$work/upstream.log" 2>&1 &
upstream=$!
for _ in $(seq 1 50); do
  curl -fsS -o /dev/null http://127.0.0.1:8765/latest.json 2>/dev/null && break
  sleep 0.2
done

XDG_STATE_HOME="$work/state" XDG_CONFIG_HOME="$work/xdg" "$BIN" \
  --config-dir "$fixture/config" --set "workspace=$work" \
  serve --port 0 --no-open --token-file "$work/url" >"$work/serve.log" 2>&1 &
server=$!
trap 'kill "$server" "$upstream" 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  [ -s "$work/url" ] && break
  sleep 0.2
done
[ -s "$work/url" ] || { cat "$work/serve.log"; echo "::error::serve did not start"; exit 1; }
url=$(cat "$work/url")
origin=${url%%/?token=*}
token=${url##*token=}
echo "serving at $origin"

# Sign in, start a dry run so there is a run page to scan, and wait for it.
jar="$work/cookies"
curl -fsS -c "$jar" -o /dev/null "$url"
csrf=$(curl -fsS -b "$jar" "$origin/settings" | sed -n 's/.*name="csrf-token" content="\([^"]*\)".*/\1/p')
[ -n "$csrf" ] || { echo "::error::no CSRF token on the settings page"; exit 1; }
# A browser's same-origin form POST carries Origin: null under the UI's
# referrer policy plus Sec-Fetch-Site: same-origin. Send exactly that, so the
# shape that was once refused with a 403 is exercised here. This is curl, not a
# click: the script has no way to drive a form in the browser axe launches.
code=$(curl -sS -b "$jar" -o /dev/null -w '%{http_code}' \
  -H "Origin: null" -H "Sec-Fetch-Site: same-origin" --data-urlencode "csrf=$csrf" \
  "$origin/check")
[ "$code" = 303 ] || { echo "::error::a browser-shaped POST /check answered $code, not 303"; exit 1; }
job=$(curl -fsS -b "$jar" -o /dev/null -w '%{redirect_url}' \
  -H "Origin: $origin" --data-urlencode "csrf=$csrf" --data "mode=dry-run" \
  "$origin/packs/icon-sets-a11y/sync")
case "$job" in
  "$origin"/jobs/*) ;;
  *) echo "::error::starting a dry run did not redirect to a run: '$job'"; exit 1 ;;
esac
job=${job#"$origin"}
for _ in $(seq 1 50); do
  curl -fsS -b "$jar" "$origin$job" | grep -q 'data-live=""' && break
  sleep 0.2
done

pages=(/ /packs/icon-sets-a11y /jobs "$job" /config /config/icon-sets-a11y.json /manifest-pack/audit /settings /about/licenses)
echo "pages: ${#pages[@]}"

status=0
for theme in light dark; do
  urls=()
  for p in "${pages[@]}"; do
    # Every URL signs in afresh: axe's browser has no cookie of its own, and
    # the token exchange keeps the theme parameter through its redirect.
    urls+=("$origin$p?token=$token&theme=$theme")
  done
  out="$work/axe-$theme.json"
  # --save is joined onto --dir, so the file name goes in one and the
  # directory in the other.
  npx --yes "@axe-core/cli@$AXE_VERSION" "${urls[@]}" \
    --chromedriver-path "$CHROMEDRIVER" \
    --tags wcag2a,wcag2aa,wcag21a,wcag21aa,wcag22aa,best-practice \
    --load-delay 500 --dir "$work" --save "axe-$theme.json" >/dev/null
  scanned=$(jq 'length' "$out")
  serious=$(jq '[.[].violations[] | select(.impact == "serious" or .impact == "critical")] | length' "$out")
  all=$(jq '[.[].violations[]] | length' "$out")
  echo "$theme: scanned $scanned pages, $all violation group(s), $serious serious or critical"
  jq -r '.[] | .url as $u | .violations[] | "  [\(.impact)] \(.id): \(.help) (\(.nodes | length) node(s)) on \($u | sub("\\?token=[^&]*&"; "?"))"' "$out"
  if [ "$scanned" -ne "${#pages[@]}" ]; then
    echo "::error::$theme: axe scanned $scanned pages, expected ${#pages[@]}"
    status=1
  fi
  if [ "$serious" -ne 0 ]; then
    echo "::error::$theme: $serious serious or critical accessibility violation(s)"
    status=1
  fi
done
exit "$status"
