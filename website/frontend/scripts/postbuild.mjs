// Runs after `ng build` on the prerendered output (dist/frontend/browser):
//
// 1. checks the performance budget: initial JavaScript at most 150 KB gzip;
// 2. checks every <img> for explicit width and height and a modern format;
// 3. preloads the fonts the first screen needs (their names carry the build hash);
// 4. writes Brotli (.br) and gzip (.gz) variants of the text assets, which the
//    back end serves to browsers that accept them (ADR 0015).
//
// Pages are not precompressed: the back end rewrites them when serving
// (canonical origin) and compresses them on the fly.
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { extname, join, relative, resolve } from 'node:path';
import { brotliCompressSync, constants, gzipSync } from 'node:zlib';

const INITIAL_JS_BUDGET = 150 * 1024;
/** Fonts of the body text and the headings above the fold; the others load on demand. */
const PRELOADED_FONTS = ['inter-latin-400-normal', 'inter-latin-700-normal'];
const IMAGE_FORMATS = ['.svg', '.webp', '.avif'];
const PRECOMPRESSED = ['.js', '.css', '.svg', '.ico', '.txt', '.json', '.xml', '.webmanifest'];
const MIN_COMPRESS_SIZE = 1024;

const dist = resolve(import.meta.dirname, '../dist/frontend/browser');
const files = walk(dist);
const pages = files.filter((file) => file.endsWith('.html'));
const problems = [];

checkInitialJs(join(dist, 'index.html'));
pages.forEach(checkImages);
const preloads = fontPreloads();
pages.forEach((page) => addPreloads(page, preloads));
const compressed = files.filter(shouldPrecompress).map(precompress);

if (problems.length > 0) {
  console.error(`postbuild: ${problems.length} problem(s):\n  ${problems.join('\n  ')}`);
  process.exit(1);
}
console.log(
  `postbuild: ${pages.length} pages with ${preloads.length} font preloads, ` +
    `${compressed.length} assets precompressed`,
);

function walk(directory) {
  return readdirSync(directory).flatMap((name) => {
    const path = join(directory, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

/** Scripts the home page loads before it is interactive: entry modules and their preloads. */
function checkInitialJs(page) {
  const html = readFileSync(page, 'utf8');
  const sources = [
    ...html.matchAll(/<script\s[^>]*src="([^"]+\.js)"/g),
    ...html.matchAll(/<link\s[^>]*rel="modulepreload"[^>]*href="([^"]+\.js)"/g),
  ].map((match) => match[1]);
  const total = [...new Set(sources)]
    .map((source) => gzipSync(readFileSync(join(dist, source)), { level: 9 }).length)
    .reduce((sum, size) => sum + size, 0);
  console.log(`postbuild: initial JavaScript ${(total / 1024).toFixed(1)} KB gzip`);
  if (sources.length === 0 || total > INITIAL_JS_BUDGET) {
    problems.push(`initial JavaScript is ${total} bytes gzip, the budget is ${INITIAL_JS_BUDGET}`);
  }
}

function checkImages(page) {
  const html = readFileSync(page, 'utf8');
  for (const [img] of html.matchAll(/<img\s[^>]*>/g)) {
    const where = `${relative(dist, page)}: ${img}`;
    if (!/\swidth="\d+"/.test(img) || !/\sheight="\d+"/.test(img)) {
      problems.push(`${where} needs explicit width and height`);
    }
    const src = /\ssrc="([^"]+)"/.exec(img)?.[1] ?? '';
    if (!IMAGE_FORMATS.includes(extname(src.split('?')[0]))) {
      problems.push(`${where} is not in a modern format (${IMAGE_FORMATS.join(', ')})`);
    }
  }
}

/** `<link rel="preload">` tags for PRELOADED_FONTS, with the hashed file names of this build. */
function fontPreloads() {
  const media = readdirSync(join(dist, 'media'));
  return PRELOADED_FONTS.map((font) => {
    const file = media.find((name) => name.startsWith(`${font}-`) && name.endsWith('.woff2'));
    if (!file) {
      problems.push(`font ${font} is missing in media/`);
      return '';
    }
    return `<link rel="preload" href="media/${file}" as="font" type="font/woff2" crossorigin>`;
  }).filter(Boolean);
}

/** Inserts the preloads after `<base>`, which the relative URLs resolve against. */
function addPreloads(page, links) {
  const html = readFileSync(page, 'utf8');
  if (html.includes('rel="preload" href="media/')) {
    return; // already done by an earlier run on the same output
  }
  const base = /<base href="\/">/.exec(html);
  if (!base) {
    problems.push(`${relative(dist, page)} has no <base href="/">`);
    return;
  }
  const end = base.index + base[0].length;
  writeFileSync(page, html.slice(0, end) + links.join('') + html.slice(end));
}

function shouldPrecompress(file) {
  return PRECOMPRESSED.includes(extname(file)) && statSync(file).size >= MIN_COMPRESS_SIZE;
}

function precompress(file) {
  const content = readFileSync(file);
  const brotli = brotliCompressSync(content, {
    params: {
      [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY,
      [constants.BROTLI_PARAM_SIZE_HINT]: content.length,
    },
  });
  writeFileSync(`${file}.br`, brotli);
  writeFileSync(`${file}.gz`, gzipSync(content, { level: 9 }));
  return file;
}
