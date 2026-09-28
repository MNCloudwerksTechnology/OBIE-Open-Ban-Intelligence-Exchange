import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { SEO_CONTENT_EN } from './seo.content';

/** Width and height from a PNG's IHDR chunk. */
function pngSize(file: Buffer): { width: number; height: number } {
  expect(file.subarray(1, 4).toString('ascii')).toBe('PNG');
  return { width: file.readUInt32BE(16), height: file.readUInt32BE(20) };
}

describe('SEO content', () => {
  const image = SEO_CONTENT_EN.shareImage;

  it('points at the committed 1200 × 630 share image and states its real size', () => {
    const file = readFileSync(resolve(process.cwd(), 'public', image.path.slice(1)));
    expect(pngSize(file)).toEqual({ width: 1200, height: 630 });
    expect([image.width, image.height]).toEqual([1200, 630]);
    expect(file.length).toBeLessThan(300 * 1024);
  });

  it('describes the share image for people who cannot see it', () => {
    expect(image.alt).toContain('Shared Intelligence, Sovereign Enforcement.');
  });

  it('avoids hype and metrics in the descriptions (brief on #1673)', () => {
    const texts = [SEO_CONTENT_EN.software.description, SEO_CONTENT_EN.notFound.description];
    for (const text of texts) {
      expect(text).not.toMatch(/revolutionary|next-generation|ai-powered|\d+ ?%/i);
    }
  });
});
