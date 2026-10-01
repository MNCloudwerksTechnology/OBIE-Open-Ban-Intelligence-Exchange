import { MeshDemoContent } from '../../content/landing-content.model';
import { PublisherId } from './mesh-demo.model';

/** Replaces the `{name}` placeholders of a content template with `values`. */
export function fill(template: string, values: Readonly<Record<string, string | number>>): string {
  return template.replace(/\{(\w+)\}/g, (placeholder, name: string) =>
    name in values ? String(values[name]) : placeholder,
  );
}

/** `value` with exactly `digits` decimals, written the way `locale` writes numbers. */
export function decimal(value: number, digits: number, locale: string): string {
  return new Intl.NumberFormat(locale, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(value);
}

/** A duration in minutes as whole days or hours, e.g. "7 days" or "1 hour". */
export function duration(minutes: number, locale: string): string {
  const [value, unit] =
    minutes % (24 * 60) === 0 ? [minutes / (24 * 60), 'day'] : [minutes / 60, 'hour'];
  return new Intl.NumberFormat(locale, { style: 'unit', unit, unitDisplay: 'long' }).format(value);
}

/** The short name of a publisher on the map and in score sums, e.g. "A" or "R". */
export function shortName(demo: MeshDemoContent, publisher: PublisherId): string {
  return publisher === 'rogue' ? demo.rogue.short : demo.servers[publisher].short;
}
