import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// Checks the colour tokens in styles.scss against WCAG 2.1 AA: 4.5:1 for
// text, 3:1 for focus rings and control borders, in both themes.

const styles = readFileSync(resolve(process.cwd(), 'src/styles.scss'), 'utf8');

/** The `--color-*` tokens declared inside `@mixin <name> { … }`. */
function themeTokens(name: string): Record<string, string> {
  const block = new RegExp(`@mixin ${name} \\{([^}]*)\\}`).exec(styles)?.[1];
  if (!block) {
    throw new Error(`mixin ${name} not found in styles.scss`);
  }
  const tokens: Record<string, string> = {};
  for (const match of block.matchAll(/--color-([a-z-]+):\s*(#[0-9a-f]{6});/gi)) {
    tokens[match[1]] = match[2];
  }
  return tokens;
}

function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
  const [r, g, b] = channels.map((c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light + 0.05) / (dark + 0.05);
}

const backgrounds = ['bg', 'surface', 'surface-alt'];
const textPairs: [string, string][] = [
  ...['text', 'text-muted', 'accent-text', 'error'].flatMap((fg) =>
    backgrounds.map((bg): [string, string] => [fg, bg]),
  ),
  ['on-accent', 'accent'],
  ['on-accent', 'accent-hover'],
  ['on-accent', 'accent-active'],
  ['code-text', 'code-bg'],
];
const nonTextPairs: [string, string][] = [
  ['accent-text', 'bg'],
  ['border-strong', 'bg'],
  ['border-strong', 'surface-alt'],
  ['border-strong', 'surface'],
  ['success', 'surface'],
  ['error', 'surface'],
];

describe.each(['light-theme', 'dark-theme'])('%s colour tokens', (theme) => {
  const tokens = themeTokens(theme);

  it.each(textPairs)('%s on %s has a text contrast of at least 4.5:1', (fg, bg) => {
    expect(contrast(tokens[fg], tokens[bg])).toBeGreaterThanOrEqual(4.5);
  });

  it.each(nonTextPairs)('%s on %s has a non-text contrast of at least 3:1', (fg, bg) => {
    expect(contrast(tokens[fg], tokens[bg])).toBeGreaterThanOrEqual(3);
  });
});
