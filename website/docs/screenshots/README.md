# Landing page screenshots

Visual check of the landing page (WP #1673) at 360 px (phone, 360×780) and
1440 px (desktop, 1440×900), light and dark theme: `*-fold.png` is the first
screen, `*-full.jpg` the whole page.

`visual-check.mjs` produces them from a running site and also prints, for
widths 320 to 2200 px, whether the page scrolls horizontally, whether the
GitHub buttons are above the fold, every request to another origin and the
axe-core (WCAG 2.1 AA) violations. Its tools are not website dependencies;
install them in a scratch directory:

```sh
make -C website && make -C website run        # site on http://localhost:8080
mkdir /tmp/visual-check && cp website/docs/screenshots/visual-check.mjs /tmp/visual-check/
cd /tmp/visual-check && npm init -y && npm install playwright-core axe-core
CHROME=/usr/bin/google-chrome node visual-check.mjs http://localhost:8080/ out
```

## Three-node demo (WP #1759)

`wp-1759/` holds the demo in "How it works" at 360 px and 1440 px, light and
dark theme, at step 4 (one voice is not enough), step 6 (C is protected
before the attack arrives) and step 8 (someone tries to abuse the mesh):
`<width>-<theme>-step<n>.png`. The screenshots are taken with reduced motion,
so they show each step's end state.

`wp-1759/demo-visual-check.mjs` produces them and, at widths 320 to 2200 px
in both themes and for every step, checks that the page does not scroll
horizontally, runs axe-core on the demo (WCAG 2.1 A/AA with colour contrast,
which jsdom cannot measure) and lists requests to other origins and console
errors. It exits non-zero when a check fails. Same tools as above:

```sh
make -C website && make -C website run        # site on http://localhost:8080
mkdir /tmp/demo-check && cp website/docs/screenshots/wp-1759/demo-visual-check.mjs /tmp/demo-check/
cd /tmp/demo-check && npm init -y && npm install playwright-core axe-core
CHROME=/usr/bin/google-chrome node demo-visual-check.mjs http://localhost:8080/ out
```
