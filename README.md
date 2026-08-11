# deployyy

The public-facing part of the [deployyy platform](https://deployyy.app):
reusable GitHub Actions build workflows and platform-managed Dockerfile
recipes per release line. A project repository on the platform is just
code plus a ~6-line caller — everything else is derived.

## Building Magento 2

```yaml
# .github/workflows/preview-build.yaml in your project repository
name: Preview build
on:
  push:
    branches-ignore: [main]
concurrency:
  group: preview-build-${{ github.ref_name }}
  cancel-in-progress: true
jobs:
  build:
    uses: ho-nl/deployyy/.github/workflows/magento2-build.yml@main
    secrets: inherit
```

The workflow:

1. **Derives the release line** from `composer.json` (never declared):
   `mage-os/product-community-edition: 3.2.*` → `mageos-320`, and so on.
2. **Builds with the platform recipe** for that line from
   [`recipes/`](recipes/) — Dockerfile + `.platform/` support files.
   Improvements to a recipe reach every consuming repository on its next
   build.
3. **Escape hatches at every level**: a repo-local `Dockerfile` wins
   wholesale; repo-local `.platform/<file>` files win per file.
4. **Per-project knobs** live in `deployyy.json`, e.g.
   `{"build": {"locales": ["nl_NL", "en_US"]}}` for the static-content
   locales (default `en_US`).
5. **Pushes commit-keyed tags** (`php-fpm-<sha7>` + `nginx-<sha7>`) to
   `ghcr.io/<your-repo>` — the deployyy operator verifies those tags and
   deploys every environment itself. Nothing deploys from CI.

One secret on your own repository: `COMPOSER_AUTH` (the contents of your
`auth.json`; the legacy name `MAGENTO_AUTH_JSON` also works). Deliberately
NOT an organization secret — the platform works for external
organizations, which never have ours.

## Recipes

| Line | Dir | Status |
|---|---|---|
| `mageos-320` | [`recipes/mageos-320/`](recipes/mageos-320/) | ✅ validated live |

A recipe row only exists after live validation. A line without a recipe
fails the build with a clear message listing what IS supported; your
repository can ship its own Dockerfile in the meantime.

## Background

The operator itself ([ho-nl/deployyy-operator](https://github.com/ho-nl/deployyy-operator))
manages environments as CRDs (branch = environment, previews with
scale-to-zero, migrations as a checkpointed state machine). This
repository is the public edge: everything a project repository — including
a third party's — needs to build on the platform.
