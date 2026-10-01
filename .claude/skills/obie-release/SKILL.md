---
name: obie-release
description: Release OBIE through its tag-triggered pipelines. The node (tag vX.Y.Z) gets a GitHub release with tarballs, SBOMs and SHA256SUMS plus the image ghcr.io/mncloudwerkstechnology/obie; the website (tag website-vX.Y.Z) gets the image ghcr.io/mncloudwerkstechnology/obie-website. Use when asked to release or tag a version, cut a release candidate, publish a container image, or find out why a release pipeline did not run or failed. In this repository it replaces the generic app-release and app-release-finalize skills.
argument-hint: "[node|website] <version, e.g. 0.1.0 or 0.1.0-rc.1>"
---

# OBIE release

CI publishes every release; nothing is uploaded by hand. Pushing a tag
starts a workflow that checks, builds and publishes. This skill brings the
repository into the state the workflows expect, pushes the tag and checks
what came out.

## What publishes what

| Tag | Workflow | Publishes |
|---|---|---|
| `vX.Y.Z` | `release.yml` | release `vX.Y.Z` (tarballs for amd64 and arm64, `*.cdx.json` SBOMs, `SHA256SUMS`) and image `obie:X.Y.Z` |
| `website-vX.Y.Z` | `website-release.yml` | image `obie-website:X.Y.Z` |

- Registry: `ghcr.io/mncloudwerkstechnology/` on GitHub; on Gitea the
  Gitea host's own registry (`<gitea-host>/<owner>/`).
- Both images are built for linux/amd64 and linux/arm64.
- `:latest` moves only for final versions. A pre-release (`X.Y.Z-rc.1`)
  publishes its own tag and leaves `:latest` alone; the node's becomes a
  GitHub pre-release.
- A tag push runs the workflow file of the tagged commit. The tag must
  point at a commit that contains the workflow.
- `ci.yml` builds both images on pull requests and on `develop`/`main` but
  never pushes them.
- Every workflow exists byte for byte in `.gitea/workflows/` and
  `.github/workflows/`; change both (`make lint-workflows` checks).

## Ground rules

- Ask the user before every step that publishes or cannot be undone:
  merging into `main`, pushing a tag, deleting a tag, changing a package's
  visibility. Approval for one step or one tag does not cover the next.
- `main` changes only through pull requests: the ruleset "Default
  Protection" requires a pull request and forbids force pushes and
  deletion. Never push to `main`.
- Merge `develop` into `main` with a merge commit, never squash or rebase,
  so the next `develop` → `main` pull request stays free of conflicts.
- Never move or reuse a tag whose release or image is published. Fix
  forward with the next patch version or the next `-rc.N`.
- GitHub needs no secrets: the workflows push with `GITHUB_TOKEN` and ask
  for `contents: write` and `packages: write` themselves. Never write a
  token into a command, file or commit.

## 1. Pick the target and the version

1. Target `node` or `website`. Their versions are independent.
2. The version is SemVer without a leading `v`: `0.1.0` or `0.1.0-rc.1`.
   `packaging/release.sh` refuses anything else.
3. List what exists; the new tag must not:

   ```sh
   git fetch origin --tags
   git ls-remote --tags origin 'v*' 'website-v*'
   gh release list
   ```

4. Cut a release candidate (`X.Y.Z-rc.1`) first for the first release, and
   whenever a workflow, `Dockerfile`, `website/Dockerfile` or anything in
   `packaging/` changed since the last release. It runs the whole pipeline
   without touching `:latest`, and it may be tagged on `develop`'s tip
   without merging into `main` first.

## 2. Node release (`vX.Y.Z`)

### 2.1 Preflight

These commands only read:

```sh
gh run list --workflow ci.yml --branch develop --limit 1   # "success" on develop's tip
grep -F '**OBIE X.Y.Z**' documentation/capabilities.md
grep -n 'vX.Y.Z' README.md documentation/getting-started.md
grep -n '^## \[' CHANGELOG.md | head -3
sed -n '/^| Date/p' documentation/operations/performance.md
```

A final version needs everything that `CONTRIBUTING.md`, section
"Releasing", lists; a release candidate skips it:

1. `documentation/capabilities.md` names **OBIE X.Y.Z** and is current.
   `make release` refuses a final version it does not name.
2. The measurements in `documentation/operations/performance.md` are
   current. They come from `make resources
   RESOURCESVERDICTS=10000,100000,1000000` (about 90 minutes) and `make
   fail2ban-versions` (Docker). Do not start them unasked: show the user
   the date in the page and ask.
3. The install commands in `README.md` and
   `documentation/getting-started.md` download `vX.Y.Z`.
4. `CHANGELOG.md`: `## [Unreleased]` becomes `## [X.Y.Z] - YYYY-MM-DD`
   under a new, empty `## [Unreleased]`. At the bottom, add
   `[X.Y.Z]: https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/tag/vX.Y.Z`
   and point `[Unreleased]` at
   `https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/compare/vX.Y.Z...develop`.

Whatever is missing goes into one pull request to `develop` (branch
`chore/release-X.Y.Z`). Wait for its CI and merge it.

### 2.2 Local dry run (optional)

```sh
make release VERSION=X.Y.Z   # tarballs, SBOMs, SHA256SUMS in dist/release/; needs GNU tar
make image VERSION=X.Y.Z     # single-arch image obie:X.Y.Z
```

This catches the capabilities gate and packaging errors before a tag
exists.

### 2.3 Merge `develop` into `main`

Skip this for a release candidate tagged on `develop`.

```sh
gh pr create --base main --head develop --title "Release vX.Y.Z" \
  --body "Brings develop into main for the vX.Y.Z release."
gh pr checks <pr> --watch
gh pr merge <pr> --merge     # merge commit; ask first
```

### 2.4 Tag

Tag the remote branch, not a local one that may be stale. Ask before the
push.

```sh
git fetch origin
git tag -a vX.Y.Z -m "OBIE X.Y.Z" origin/main        # release candidate on develop: origin/develop
git show --no-patch vX.Y.Z                           # the merge commit from 2.3
git ls-tree vX.Y.Z .github/workflows/release.yml     # the workflow is in the tagged commit
git push origin vX.Y.Z
```

### 2.5 Watch

```sh
gh run list --event push --limit 5     # the run "release" with branch vX.Y.Z
gh run watch <run-id> --exit-status
```

It takes up to 40 minutes, in this order: `make ci`, `make release`,
`make check-unit`, image build and push, then
`packaging/publish-release.sh`. The release appears only once the image is
pushed.

### 2.6 Verify

```sh
gh release view vX.Y.Z    # obie-X.Y.Z-linux-{amd64,arm64}.tar.gz, *.cdx.json, SHA256SUMS
docker buildx imagetools inspect ghcr.io/mncloudwerkstechnology/obie:X.Y.Z   # linux/amd64 and linux/arm64
docker run --rm ghcr.io/mncloudwerkstechnology/obie:X.Y.Z --version         # prints X.Y.Z
```

For a final version, `imagetools inspect` of `:latest` shows the same
digest as `:X.Y.Z`.

### 2.7 First release only: make the package public

A new package under the personal account `MNCloudwerksTechnology` is
private although the repository is public: it inherits the repository's
access permissions, not its visibility. GitHub's REST API cannot change
visibility, so hand this to the user: github.com → profile → Packages →
`obie` → Package settings → Danger Zone → Change visibility → Public. This
cannot be undone. Then pull without credentials:

```sh
DOCKER_CONFIG=$(mktemp -d) docker pull ghcr.io/mncloudwerkstechnology/obie:X.Y.Z
```

## 3. Website release (`website-vX.Y.Z`)

### 3.1 Preflight

- The website changes are merged into `develop`, and `develop` into
  `main` (2.3), unless this is a release candidate tagged on `develop`.
- CI is green on the commit to tag. Its `website` job ran
  `make -C website ci` and `make -C website smoke` when `website/` changed.
- `git ls-remote --tags origin 'website-v*'` shows the last version; pick
  the next. There is no changelog or capabilities gate.

### 3.2 Tag

Ask before the push.

```sh
git fetch origin
git tag -a website-vX.Y.Z -m "OBIE website X.Y.Z" origin/main
git ls-tree website-vX.Y.Z .github/workflows/website-release.yml
git push origin website-vX.Y.Z
```

### 3.3 Watch and verify

As in 2.5; the run is "website-release" and takes up to 45 minutes
(`make -C website ci`, the smoke test, then the image push). Then:

```sh
docker buildx imagetools inspect ghcr.io/mncloudwerkstechnology/obie-website:X.Y.Z
```

### 3.4 First release only: make the package public

As in 2.7, for the package `obie-website`. If it stays private, the server
needs `docker login ghcr.io` before it can pull
(`website/deploy/README.md`).

### 3.5 Deploy

Deploying is not part of CI; do it only when the user asks and has access
to the server. Follow "Updating" in `website/deploy/README.md`: take a
backup, set `OBIE_IMAGE=ghcr.io/mncloudwerkstechnology/obie-website:X.Y.Z`
in `.env`, then `docker compose pull` and `docker compose up -d --wait`.
Flyway migrations only go forward, so the backup comes first.

## 4. When it fails

| Symptom | Cause | Fix |
|---|---|---|
| No run starts after the tag push | The tagged commit lacks the workflow, or the tag does not match `v*`/`website-v*` | `git ls-tree <tag> .github/workflows/`; delete the tag (below) and tag a commit that has the workflow |
| `release.sh: documentation/capabilities.md does not describe OBIE X.Y.Z` | The capabilities gate (2.1) | Update the page through a pull request, delete the tag, tag again |
| `make ci`, `make check-unit` or the smoke test fails | A flaky test, or a real defect | Flaky: `gh run rerun <run-id> --failed`. Real: fix through `develop` (and `main`), delete the tag, tag again |
| `denied: permission_denied: write_package` on push | The package was first pushed with a personal token and the repository may not write it | Package settings → Manage Actions access → add the repository with role Write, then rerun |
| `publish-release.sh` fails | The image is already pushed | `gh run rerun <run-id>`: the script reuses an existing release and uploads only missing files |

Delete a tag only when nothing was published for it, that is, when both
`gh release view <tag>` and `docker buildx imagetools inspect <image>:X.Y.Z`
fail. Ask first:

```sh
git push origin :refs/tags/vX.Y.Z
git tag -d vX.Y.Z
```

Once a release or an image exists, fix forward with a new version instead.

## 5. Gitea

The same workflows run on Gitea Actions when a tag reaches the Gitea
repository: the image goes to the Gitea host's registry and the node's
release to Gitea. That needs a runner with Docker online and, where the
workflow token cannot push packages, a repository secret `REGISTRY_TOKEN`
with package write access. Whether tags reach Gitea depends on how the
mirror between Gitea and GitHub is set up; check with the user.
