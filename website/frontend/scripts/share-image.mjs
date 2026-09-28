// Generates the share image (Open Graph / Twitter card) public/social/obie-share.png,
// 1200 × 630 px, with headless Chrome: dark background, the OBIE mark and the
// tagline, set in the self-hosted Inter. The PNG is committed; run this again
// only when the design changes:
//
//   npm run share-image            # CHROME=/path/to/chrome to pick the browser
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const WIDTH = 1200;
const HEIGHT = 630;
const root = resolve(import.meta.dirname, '..');
const output = join(root, 'public/social/obie-share.png');
const chrome = process.env.CHROME ?? 'google-chrome';

const font = (weight) =>
  pathToFileURL(
    join(root, `node_modules/@fontsource/inter/files/inter-latin-${weight}-normal.woff2`),
  );
// The mark from public/brand/obie-logo-solo.svg; the outer ring is lightened
// so it stays visible on the dark background.
const mark = readFileSync(join(root, 'public/brand/obie-logo-solo.svg'), 'utf8')
  .replace(/<!--.*?-->/g, '')
  .replace('stroke="#333333"', 'stroke="#4A4A4A"');

const html = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><style>
  @font-face { font-family: Inter; font-weight: 400; src: url(${font(400)}) format('woff2'); }
  @font-face { font-family: Inter; font-weight: 700; src: url(${font(700)}) format('woff2'); }
  html, body { margin: 0; width: ${WIDTH}px; height: ${HEIGHT}px; overflow: hidden; }
  body { display: flex; align-items: center; gap: 64px; box-sizing: border-box; padding: 0 96px;
         background: #1A1A1A; color: #F5F5F5; font-family: Inter, sans-serif; }
  svg { width: 300px; height: 300px; flex: none; }
  .name { margin: 0; color: #FE8A07; font-size: 136px; font-weight: 700; letter-spacing: 0.04em; line-height: 1; }
  .expansion { margin: 16px 0 0; color: #BDBDBD; font-size: 26px; letter-spacing: 0.12em; text-transform: uppercase; }
  .tagline { margin: 48px 0 0; padding-top: 32px; border-top: 3px solid #FE8A07;
             font-size: 42px; font-weight: 700; line-height: 1.2; }
</style></head><body>
  ${mark}
  <div>
    <p class="name">OBIE</p>
    <p class="expansion">Open Ban Intelligence Exchange</p>
    <p class="tagline">Shared Intelligence,<br>Sovereign Enforcement.</p>
  </div>
</body></html>`;

const work = mkdtempSync(join(tmpdir(), 'obie-share-'));
try {
  const page = join(work, 'share.html');
  writeFileSync(page, html);
  execFileSync(chrome, [
    '--headless',
    '--disable-gpu',
    '--hide-scrollbars',
    '--force-device-scale-factor=1',
    '--allow-file-access-from-files',
    `--user-data-dir=${join(work, 'profile')}`,
    `--window-size=${WIDTH},${HEIGHT}`,
    '--virtual-time-budget=2000',
    `--screenshot=${output}`,
    pathToFileURL(page).href,
  ]);
  console.log(`wrote ${output}`);
} finally {
  rmSync(work, { recursive: true, force: true });
}
