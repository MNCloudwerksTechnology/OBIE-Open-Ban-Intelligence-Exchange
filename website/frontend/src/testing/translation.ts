// Helpers for the tests that compare a translation with the English copy.

/** Every string in `value`, depth first. */
export function allStrings(value: unknown): string[] {
  if (typeof value === 'string') {
    return [value];
  }
  if (value && typeof value === 'object') {
    return Object.values(value).flatMap(allStrings);
  }
  return [];
}

/** Every `[path, value]` pair of `value`'s leaves, e.g. `['$.hero.primary.href', 'https://…']`. */
export function leaves(value: unknown, path = '$'): [string, unknown][] {
  if (value && typeof value === 'object') {
    return Object.entries(value).flatMap(([key, child]) => leaves(child, `${path}.${key}`));
  }
  return [[path, value]];
}

/**
 * Where `translated` has another shape than `source`: other keys, in another
 * order, other array lengths or other kinds of value. Empty when they match.
 */
export function shapeDifferences(source: unknown, translated: unknown, path = '$'): string[] {
  const kind = (value: unknown) => (Array.isArray(value) ? 'array' : typeof value);
  if (kind(source) !== kind(translated)) {
    return [`${path}: ${kind(source)} became ${kind(translated)}`];
  }
  if (Array.isArray(source) && Array.isArray(translated)) {
    if (source.length !== translated.length) {
      return [`${path}: ${source.length} items became ${translated.length}`];
    }
    return source.flatMap((item, i) => shapeDifferences(item, translated[i], `${path}[${i}]`));
  }
  if (source && typeof source === 'object' && translated && typeof translated === 'object') {
    const keys = Object.keys(source);
    const translatedKeys = Object.keys(translated);
    if (keys.join() !== translatedKeys.join()) {
      return [`${path}: keys ${keys.join()} became ${translatedKeys.join()}`];
    }
    return keys.flatMap((key) =>
      shapeDifferences(
        (source as Record<string, unknown>)[key],
        (translated as Record<string, unknown>)[key],
        `${path}.${key}`,
      ),
    );
  }
  return [];
}

/** The `{name}` placeholders of a string, sorted. */
export function placeholders(text: string): string[] {
  return [...new Set(text.match(/\{\w+\}/g) ?? [])].sort();
}

/** Number of words, as a reader counts them. */
export function words(text: string): number {
  return text.trim().split(/\s+/).length;
}
