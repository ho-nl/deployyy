# deployyy

deployyy runs your Magento 2 project on the [deployyy platform](https://deployyy.app).
Each git branch becomes an environment. Preview environments scale to zero
when idle. Your repository contains only code and a short caller workflow —
the platform derives all other data.

This repository is the public part of the platform. It contains the build
workflows and the Dockerfile recipes.

## Get started

### Step 1 — Link your repository

Install the deployyy GitHub App on your repository:
**[github.com/apps/deployyy-platform](https://github.com/apps/deployyy-platform)**.

The platform then connects your repository and prepares your environments.
Contact us if your organization is not on the platform yet.

### Step 2 — Add the `COMPOSER_AUTH` secret

Add one Actions secret to your repository: `COMPOSER_AUTH`. Its value is
the content of your `auth.json` (your Magento Marketplace or Packagist
keys). Composer reads this variable natively.

The platform does not use organization secrets. Your keys stay in your
repository.

### Step 3 — Add the build workflows

Add two caller workflows to your repository:

```yaml
# .github/workflows/preview-build.yaml — builds every branch
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

```yaml
# .github/workflows/build.yaml — builds main
name: Build
on:
  push:
    branches: [main]
concurrency:
  group: build-${{ github.ref_name }}
  cancel-in-progress: false
jobs:
  build:
    uses: ho-nl/deployyy/.github/workflows/magento2-build.yml@main
    secrets: inherit
```

### Step 4 — Push a branch

Push a branch. The workflow builds two images and pushes them to
`ghcr.io/<your-repo>` with commit tags (`php-fpm-<sha7>`, `nginx-<sha7>`).
The operator finds the tags and deploys your preview environment at
`https://<branch>.<project>.deployyy.app`. CI does not deploy — the
operator does.

## Configuration

Your repository contains no platform configuration file. The platform
holds your project configuration:

- The release line comes from your `composer.json`. You do not declare it.
- Your service stack (database, search, queue, cache) is part of your
  project configuration on the platform.
- The static-content locales come from the repository variable
  `DEPLOYYY_MAGENTO_LOCALES`. The platform sets this variable from your
  project configuration. The default is `en_US`. Do not edit the variable
  by hand — the platform converges it.

To change your configuration, contact the platform team. A dashboard for
self-service configuration is planned.

## How the build works

1. The workflow reads `composer.json` and finds your release line.
   Example: `mage-os/product-community-edition: 3.2.*` gives `mageos-320`.
   You do not declare the line.
2. It builds with the platform recipe for that line from
   [`recipes/`](recipes/): a Dockerfile plus `.platform/` support files.
   When we improve a recipe, your project gets the improvement on its
   next build.
3. You can replace the recipe. A `Dockerfile` in your repository replaces
   the full recipe Dockerfile. A file in your `.platform/` directory
   replaces only that one file.

## Recipes

| Line | Dir | Status |
|---|---|---|
| `mageos-320` | [`recipes/mageos-320/`](recipes/mageos-320/) | ✅ validated live |

A recipe is added only after a live validation. When a line has no
recipe, the build stops with a clear message that shows the supported
lines. Your repository can ship its own Dockerfile until the recipe is
available.

## Background

The [deployyy operator](https://github.com/ho-nl/deployyy-operator)
manages environments as Kubernetes resources: branch = environment,
previews with scale-to-zero, database migrations as a checkpointed state
machine, and a live development mode per preview. This repository is the
public edge of that platform.
