# Agent rules: deployyy

Build recipes (`recipes/<line>/`: a Dockerfile plus `.platform/` support files)
that the hosted build of deployyy-operator (`internal/build`) uses.
[README.md](README.md) is the customer documentation of the recipes.

## Validate before a push

```bash
actionlint -color
shellcheck recipes/*/platform/*.sh recipes/*/platform/sendmail-tagged recipes/*/platform/bin/*
for f in recipes/*/platform/*.php; do php -l "$f"; done
for f in graphcommerce/*.mjs recipes/*/platform/*.mjs; do node --check "$f"; done
```

`.github/workflows/lint.yml` runs the same checks and fails when a shared
Magento support file differs between `recipes/mageos-3/platform/` and
`recipes/magento/platform/` (`nginx-default.conf`, `sendmail-tagged`,
`check-patches.php`, `config-check-off.php`). Change both copies.

A recipe change applies to every hosted project on its next build. Test a
change against a real project build before the merge: annotate the project's
Repository `deployyy.app/recipes-ref: <commit>`.

## Programs

`recipes/<recipe>/platform/bin/` holds the programs of the processes and the
release steps of a recipe. `.github/workflows/programs.yml` pushes
`ghcr.io/ho-nl/deployyy-programs:<commit>` on each push to `main`. A recipe
version names that image in `programs`, and the operator mounts the folder of
the recipe at `/platform/bin/` for hosted and manual images alike
(deployyy-operator `docs/RECIPES-AND-RELEASES.md` §3.3).

## Rules

- `env.php` is the platform's. A project sets per-environment configuration
  with `CONFIG__…` variables, never in `env.php`.
- Build variables reach the build only. The running application reads its
  variables from the platform (deployyy-operator `docs/VARIABLES.md`).
- The operator reads the image labels `app.deployyy.*`. A new label is a
  contract with the operator: add it there in the same change.
- Every sentence a recipe prints follows the copy rules in
  deployyy-operator `AGENTS.md`.
