// Test helpers: jsdom has no matchMedia, so tests install a fake one.

const DARK = '(prefers-color-scheme: dark)';
const REDUCED_MOTION = '(prefers-reduced-motion: reduce)';

/** A controllable media query, e.g. `prefers-color-scheme: dark`. */
export class FakeDarkQuery {
  private readonly listeners: ((event: { matches: boolean }) => void)[] = [];

  constructor(public matches: boolean) {}

  addEventListener(_type: 'change', listener: (event: { matches: boolean }) => void): void {
    this.listeners.push(listener);
  }

  /** Simulates the visitor changing the system setting. */
  change(matches: boolean): void {
    this.matches = matches;
    this.listeners.forEach((listener) => listener({ matches }));
  }
}

/**
 * Installs a fake matchMedia (jsdom has none) that answers the two queries
 * the site asks: the colour scheme and reduced motion (the demo, ADR 0028).
 */
export function stubMediaQueries(settings: { dark?: boolean; reducedMotion?: boolean }): {
  dark: FakeDarkQuery;
  reducedMotion: FakeDarkQuery;
} {
  const queries = {
    dark: new FakeDarkQuery(settings.dark ?? false),
    reducedMotion: new FakeDarkQuery(settings.reducedMotion ?? false),
  };
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => {
      expect([DARK, REDUCED_MOTION]).toContain(media);
      return media === DARK ? queries.dark : queries.reducedMotion;
    }),
  );
  return queries;
}

/** Installs a fake matchMedia (jsdom has none) and returns its dark query. */
export function stubSystemTheme(dark: boolean): FakeDarkQuery {
  return stubMediaQueries({ dark }).dark;
}
