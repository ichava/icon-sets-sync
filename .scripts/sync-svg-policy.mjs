#!/usr/bin/env node
/**
 * Sync the canonical SVG policy into every runtime that has to enforce it.
 *
 * Four runtimes sanitise SVG in this ecosystem and none of them can reach the
 * others at build time: `browser` resolves `ichava/core` from a published tag
 * rather than the sibling tree, and `react-browser` and `icon-sets-sync`
 * have no Composer dependency on core at all. So each vendors a byte-identical
 * copy, and this script is what makes those copies trustworthy rather than
 * merely present.
 *
 *   node .scripts/sync-svg-policy.mjs           # check, exit 1 on drift
 *   node .scripts/sync-svg-policy.mjs --write   # copy canonical over the rest
 *
 * Run from the workspace root, or set ICHAVA_ROOT. This is the cross-repo gate:
 * a per-repo test can only pin its own copy against a recorded digest, which
 * catches accidental edits but cannot see that core has moved on. This can,
 * because it is the one place all the checkouts are visible at once.
 */

import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

const ROOT = process.env.ICHAVA_ROOT || process.cwd();
const WRITE = process.argv.includes('--write');

const CANONICAL = join(ROOT, 'core/resources/security/svg-policy.json');

// The repository was renamed maintainer-toolkit -> icon-sets-sync on
// 2026-10-21, and a checkout made before that keeps the old directory name.
const TOOLKIT = existsSync(join(ROOT, 'icon-sets-sync')) ? 'icon-sets-sync' : 'maintainer-toolkit';

const CONSUMERS = [
  'icon-browser/resources/js/core/svg-policy.json',
  'react-icon-browser/src/core/svg-policy.json',
  // //go:embed cannot reach outside the package directory, so the Go module
  // carries its own copy.
  `${TOOLKIT}/src/internal/core/svg/svg-policy.json`,
];

const sha = (buf) => createHash('sha256').update(buf).digest('hex');

if (!existsSync(CANONICAL)) {
  console.error(`canonical policy not found: ${CANONICAL}`);
  console.error('run from the workspace root, or set ICHAVA_ROOT');
  process.exit(2);
}

const canonical = readFileSync(CANONICAL);
const want = sha(canonical);

console.log(`canonical  ${CANONICAL.replace(ROOT + '/', '')}`);
console.log(`sha256     ${want}\n`);

let drift = 0;
let missing = 0;

for (const rel of CONSUMERS) {
  const path = join(ROOT, rel);

  if (!existsSync(path)) {
    if (WRITE) {
      writeFileSync(path, canonical);
      console.log(`CREATED  ${rel}`);
    } else {
      console.log(`MISSING  ${rel}`);
      missing++;
    }
    continue;
  }

  const got = sha(readFileSync(path));

  if (got === want) {
    console.log(`ok       ${rel}`);
    continue;
  }

  if (WRITE) {
    writeFileSync(path, canonical);
    console.log(`UPDATED  ${rel}`);
  } else {
    console.log(`DRIFT    ${rel}`);
    console.log(`         has ${got}`);
    drift++;
  }
}

if (!WRITE && (drift || missing)) {
  console.error(`\n${drift} drifted, ${missing} missing. Re-run with --write, then update the pinned digest in each runtime's policy test.`);
  process.exit(1);
}

if (WRITE) {
  console.log(`\nCopies synced. Update the pinned digest in each runtime's policy test to:\n  ${want}`);
}
