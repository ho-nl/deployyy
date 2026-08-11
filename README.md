# deployyy

This is the public part of the [deployyy platform](https://deployyy.app).
It contains reusable GitHub Actions build workflows. It also contains the
platform Dockerfile recipes for each release line. A project repository
on the platform contains only code and a short caller workflow. The
platform derives all other data.

## Build Magento 2

Add this caller workflow to your project repository:

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

The workflow does these steps:

1. It reads `composer.json` and finds the release line. You do not
   declare the release line. Example: `mage-os/product-community-edition:
   3.2.*` gives the line `mageos-320`.
2. It builds your project with the platform recipe for that line. The
   recipes are in [`recipes/`](recipes/). Each recipe contains a
   Dockerfile and `.platform/` support files. When we improve a recipe,
   each project gets the improvement on its next build.
3. You can replace the recipe. A `Dockerfile` in your repository replaces
   the full recipe Dockerfile. A file in your `.platform/` directory
   replaces only that one file.
4. You can set project options in `deployyy.json`. Example:
   `{"build": {"locales": ["nl_NL", "en_US"]}}` sets the static-content
   locales. The default locale is `en_US`.
5. It pushes two images to `ghcr.io/<your-repo>`. The tags contain the
   commit: `php-fpm-<sha7>` and `nginx-<sha7>`. The deployyy operator
   finds these tags and deploys each environment. CI does not deploy.

Set one secret on your repository: `COMPOSER_AUTH`. Its value is the
content of your `auth.json`. The old name `MAGENTO_AUTH_JSON` also works.
The platform does not use organization secrets: external organizations do
not have them.

## Recipes

| Line | Dir | Status |
|---|---|---|
| `mageos-320` | [`recipes/mageos-320/`](recipes/mageos-320/) | ✅ validated live |

A recipe is added only after a live validation. When a line has no
recipe, the build stops with a clear message. The message shows the
supported lines. Your repository can ship its own Dockerfile until the
recipe is available.

## Background

The [deployyy operator](https://github.com/ho-nl/deployyy-operator)
manages environments as Kubernetes resources. Each branch is an
environment. Preview environments scale to zero. Migrations are a
checkpointed state machine. This repository is the public edge of that
platform. It contains everything a project repository needs for a build
on the platform — also for third parties.
