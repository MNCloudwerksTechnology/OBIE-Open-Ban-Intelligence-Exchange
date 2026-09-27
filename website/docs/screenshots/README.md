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
