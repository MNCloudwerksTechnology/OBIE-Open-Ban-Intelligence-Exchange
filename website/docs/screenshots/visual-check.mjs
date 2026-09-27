// Visual and accessibility check of the landing page: screenshots at 360 px
// and 1440 px in both themes, plus horizontal-scroll, above-the-fold GitHub
// button, third-party request and axe-core checks at five widths.
// Usage (see README.md): node visual-check.mjs [base-url] [output-dir]
import { chromium } from 'playwright-core';
import { readFileSync, mkdirSync } from 'node:fs';
const base = process.argv[2] ?? 'http://localhost:8080/';
const out = process.argv[3] ?? 'out';
mkdirSync(out, { recursive: true });
const axe = readFileSync(new URL(import.meta.resolve('axe-core/axe.min.js')), 'utf8');
const browser = await chromium.launch({ executablePath: process.env.CHROME ?? '/usr/bin/google-chrome' });
const origin = new URL(base).origin;
for (const [w, h] of [[360, 780], [1440, 900], [320, 640], [768, 1024], [2200, 1200]]) {
  for (const scheme of ['light', 'dark']) {
    const ctx = await browser.newContext({ viewport: { width: w, height: h }, colorScheme: scheme });
    const page = await ctx.newPage();
    const foreign = [];
    const errors = [];
    page.on('request', (r) => { if (!r.url().startsWith(origin) && !r.url().startsWith('data:')) foreign.push(r.url()); });
    page.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') errors.push(m.text()); });
    await page.goto(base, { waitUntil: 'networkidle' });
    await page.evaluate(() => document.fonts.ready);
    const info = await page.evaluate(() => {
      const btns = [...document.querySelectorAll('a')].filter((a) => /GitHub/.test(a.textContent ?? ''));
      const vis = btns.map((b) => { const r = b.getBoundingClientRect(); return { text: b.textContent.trim(), top: Math.round(r.top), bottom: Math.round(r.bottom), inView: r.bottom <= innerHeight && r.top >= 0 && r.width > 0 }; }).slice(0, 2);
      return { hscroll: document.documentElement.scrollWidth > document.documentElement.clientWidth, sw: document.documentElement.scrollWidth, github: vis, fonts: [...document.fonts].filter(f => f.status === 'loaded').map(f => f.family + f.weight) };
    });
    const tag = `${w}-${scheme}`;
    if (w === 360 || w === 1440) {
      await page.screenshot({ path: `${out}/${tag}-fold.png` });
      await page.screenshot({ path: `${out}/${tag}-full.png`, fullPage: true });
    }
    await page.addScriptTag({ content: axe });
    const res = await page.evaluate(async () => { const r = await axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21aa', 'best-practice'] }); return r.violations.map(v => `${v.id} (${v.nodes.length}): ${v.nodes.slice(0,3).map(n => n.target.join(' ')).join(' | ')}`); });
    console.log(tag, JSON.stringify({ ...info, foreign, errors, axe: res }));
    await ctx.close();
  }
}
await browser.close();
