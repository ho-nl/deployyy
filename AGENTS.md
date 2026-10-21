# Agent rules: deployyy

Build recipes (`recipes/<line>/`: a Dockerfile plus `.platform/` support files)
and the reusable GitHub workflows (`.github/workflows/*-build.yml`) that project
repositories call. [README.md](README.md) is the customer documentation and the
contract of the workflows.

## Validate before a push

```bash
actionlint -color
shellcheck recipes/*/platform/*.sh recipes/*/platform/sendmail-tagged
for f in recipes/*/platform/*.php; do php -l "$f"; done
for f in graphcommerce/*.mjs recipes/*/platform/*.mjs; do node --check "$f"; done
```

`.github/workflows/lint.yml` runs the same checks and fails when a shared
Magento support file differs between `recipes/mageos-3/platform/` and
`recipes/magento/platform/` (`nginx-default.conf`, `sendmail-tagged`,
`check-patches.php`, `config-check-off.php`). Change both copies.

A recipe change applies to every project on its next build. Test a change
against a real project build before the merge: run the project's workflow
with `recipes-ref` set to your branch.

## Rules

- `env.php` is the platform's. A project sets per-environment configuration
  with `CONFIG__…` variables, never in `env.php`.
- GitHub values reach the build only. The running application reads its
  variables from the platform (deployyy-operator `docs/VARIABLES.md`).
- A caller workflow names no secret and no variable: `vars: ${{ toJSON(vars) }}`
  and `secrets: all: ${{ toJSON(secrets) }}`.
- The operator reads the image labels `app.deployyy.*`. A new label is a
  contract with the operator: add it there in the same change.
- Every sentence the workflows print follows the copy rules in
  deployyy-operator `AGENTS.md`.
