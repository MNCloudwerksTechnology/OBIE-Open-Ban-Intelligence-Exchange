// Visual and accessibility check of the three-node demo (WP #1759): screenshots
// of steps 4, 6 and 8 at 360 px and 1440 px in both themes, and at six widths
// from 320 to 2200 px, for every step: horizontal scrolling, axe-core
// (WCAG 2.1 A/AA, colour contrast included), requests to other origins and
// console errors. Exits non-zero when any check fails.
// Usage (see README.md): node demo-visual-check.mjs [base-url] [output-dir]
import { chromium } from 'playwright-core';
import { mkdirSync, readFileSync } from 'node:fs';

const base = process.argv[2] ?? 'http://localhost:8080/';
const out = process.argv[3] ?? 'out';
mkdirSync(out, { recursive: true });
const axe = readFileSync(new URL(import.meta.resolve('axe-core/axe.min.js')), 'utf8');
const browser = await chromium.launch({
  executablePath: process.env.CHROME ?? '/usr/bin/google-chrome',
});
const origin = new URL(base).origin;
const SHOTS = [4, 6, 8];
const STEPS = 10;
let problems = 0;

async function openDemo(width, scheme) {
  const context = await browser.newContext({
    viewport: { width, height: 900 },
    colorScheme: scheme,
    // Screenshots and contrast checks of the end state, not of a frame mid-animation.
    reducedMotion: 'reduce',
  });
  const page = await context.newPage();
  const foreign = [];
  const errors = [];
  page.on('request', (r) => {
    if (!r.url().startsWith(origin) && !r.url().startsWith('data:')) foreign.push(r.url());
  });
  page.on('pageerror', (e) => errors.push(String(e)));
  page.on('console', (m) => {
    // Without the back end (a static server), /api/project answers 404; that is not the demo.
    if (m.type() === 'error' && !m.text().includes('404')) errors.push(m.text());
  });
  await page.goto(base, { waitUntil: 'networkidle' });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForSelector('app-mesh-demo .toolbar');
  await page.addScriptTag({ content: axe });
  return { context, page, foreign, errors };
}

async function goTo(page, step) {
  await page.locator('app-mesh-demo .steps button').nth(step - 1).click();
  await page.waitForSelector(`app-mesh-demo .steps li:nth-child(${step}) [aria-current="step"]`);
}

for (const width of [320, 360, 768, 1024, 1440, 2200]) {
  for (const scheme of ['light', 'dark']) {
    const { context, page, foreign, errors } = await openDemo(width, scheme);
    const report = { width, scheme, hscroll: [], axe: [] };
    for (let step = 1; step <= STEPS; step++) {
      await goTo(page, step);
      const scrolls = await page.evaluate(
        () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
      );
      if (scrolls) report.hscroll.push(step);
      const violations = await page.evaluate(async () => {
        const result = await axe.run(document.querySelector('app-mesh-demo'), {
          runOnly: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'],
        });
        return result.violations.map(
          (v) => `${v.id} (${v.nodes.length}): ${v.nodes.slice(0, 3).map((n) => n.target.join(' ')).join(' | ')}`,
        );
      });
      report.axe.push(...violations.map((v) => `step ${step}: ${v}`));
      if ((width === 360 || width === 1440) && SHOTS.includes(step)) {
        await page.locator('app-mesh-demo').screenshot({
          path: `${out}/${width}-${scheme}-step${step}.png`,
        });
      }
    }
    const failed = report.hscroll.length + report.axe.length + foreign.length + errors.length;
    problems += failed;
    console.log(failed ? 'FAIL' : 'ok  ', JSON.stringify({ ...report, foreign, errors }));
    await context.close();
  }
}
await browser.close();
process.exit(problems ? 1 : 0);
