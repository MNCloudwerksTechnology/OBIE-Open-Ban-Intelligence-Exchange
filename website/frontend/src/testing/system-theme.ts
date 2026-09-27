// Test helpers: jsdom has no matchMedia, so tests install a fake one.

/** A controllable `prefers-color-scheme: dark` media query. */
export class FakeDarkQuery {
  private readonly listeners: ((event: { matches: boolean }) => void)[] = [];

  constructor(public matches: boolean) {}

  addEventListener(_type: 'change', listener: (event: { matches: boolean }) => void): void {
    this.listeners.push(listener);
  }

  /** Simulates the visitor changing the system theme. */
  change(matches: boolean): void {
    this.matches = matches;
    this.listeners.forEach((listener) => listener({ matches }));
  }
}

/** Installs a fake matchMedia (jsdom has none) and returns its dark query. */
export function stubSystemTheme(dark: boolean): FakeDarkQuery {
  const query = new FakeDarkQuery(dark);
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => {
      expect(media).toBe('(prefers-color-scheme: dark)');
      return query;
    }),
  );
  return query;
}
