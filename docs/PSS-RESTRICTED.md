# Running recipe images under Pod Security Standard `restricted`

Status: the images are ready; the operator does not enforce `restricted` yet.
This page says what the images do and what the operator must change.

## Why

Environment namespaces enforce `baseline` and only warn on `restricted`
(deployyy-operator `internal/composition/environment.go`). Two things in the
images kept them from `restricted`:

- **php-fpm** (thecodingmachine base): the entrypoint re-executes itself
  through `sudo` to write PHP config from `PHP_EXTENSION_*` / `PHP_INI_*`
  variables, and `/usr/bin/php` does the same on every CLI call. `sudo` needs
  privilege escalation, which `restricted` forbids (`allowPrivilegeEscalation:
  false` sets `no_new_privs`, and `sudo` then refuses to run).
- **nginx** (`nginx:alpine`): the master process runs as root.

With open sign-up, customer code runs in these pods. `restricted` is the
standard for untrusted code.

## What the images do now

| Recipe | Image | User | Notes |
|---|---|---|---|
| `recipes/magento` (2.4.6–2.4.9) | `php-fpm-<sha>` | `1000:1000` (`docker`) | Sudo-less entrypoint and `php` wrapper; `sudo` removed from the user |
| `recipes/magento` | `nginx-<sha>` | `101:101` (runs as any uid) | `nginxinc/nginx-unprivileged`, listens on 8080 |
| `recipes/nextjs` (Next.js, GraphCommerce) | `sha-<sha>` | `1001:1001` (`nextjs`) | `node server.js` on 3000 |
| `recipes/nextjs` | `sha-<sha>-cache-seed` | `1001:1001` | busybox; the seed Job already runs as 1001 |

All users are **numeric** in the image, so the kubelet can check
`runAsNonRoot: true` even when a pod sets no `runAsUser`. No image needs a
capability, a setuid binary or a writable root file system path outside its
own user's files.

### The php-fpm entrypoint

`recipes/magento/platform/php-entrypoint.sh` replaces thecodingmachine's
`docker-entrypoint.sh`. It does the parts the platform uses, as the app user:

1. writes `generated_conf.ini` from `PHP_INI_*` variables and enables or
   disables extensions from `PHP_EXTENSION_*` variables. The Dockerfile made
   those files (`/etc/php/<v>/{cli,fpm}/conf.d`, `/var/lib/php/modules`,
   `generated_conf.ini`, `/opt/php_env_var_cache.php`) owned by uid 1000;
2. runs `STARTUP_COMMAND_*` variables;
3. `exec`s the command (default `php-fpm`).

It leaves out the uid remapping and host detection for Docker Desktop, and
the `CRON_*` launcher: the platform runs supercronic as its own container.

`/usr/bin/php` (`php-wrapper.sh`) rebuilds the config when `PHP_*`
variables changed, without `sudo`. So a pod or Job that overrides the
command (cron, consumers, the migration Jobs, `kubectl exec`) still gets its
own `PHP_INI_*` values — dev mode's opcache settings keep working.

The php.ini template is `production` (the base image defaults to
`development`, which displays errors).

Verified locally: the image starts and serves with `--user 1000:1000
--cap-drop ALL --security-opt no-new-privileges`, and a runtime
`PHP_INI_MEMORY_LIMIT` takes effect.

### Scope

- `recipes/mageos-3` and `recipes/laravel` are **unchanged**: they are
  validated live on their current images, and the change needs a live
  validation first. Port the same four pieces (the two scripts, the ownership
  step, nginx-unprivileged) once `recipes/magento` has run live.
- A project with its own `Dockerfile` decides for itself.

## What the operator must change to enforce `restricted`

1. **Pod security context** for the workloads that run recipe images
   (`podsec.Harden` has the shape):
   - Magento web pod (php + nginx), cron and consumers: `runAsUser: 1000`,
     `runAsGroup: 1000`, `fsGroup: 1000`, `runAsNonRoot: true`. nginx
     accepts uid 1000 (verified), so the pod needs one uid; 1000 also owns
     the media volume (`medianfs.go` chowns the export to 1000).
   - GraphCommerce / Next.js Deployment: `runAsUser: 1001`, `runAsGroup:
     1001`, `fsGroup: 1001`, `runAsNonRoot: true`; the seed Job already sets
     1001.
   - Every container: `allowPrivilegeEscalation: false`,
     `capabilities: {drop: [ALL]}`, `seccompProfile: {type: RuntimeDefault}`.
2. **Know which image a release runs.** Images from `recipes/mageos-3`,
   `recipes/laravel` or a project's own Dockerfile still run as root. Every
   restricted-ready recipe image carries the label
   `app.deployyy.pod-security=restricted`; the operator can read it from the
   image config when it verifies a tag (one more registry GET next to the
   manifest HEAD in `internal/gh/registry.go`) and set the context only then.
   Without that check, a hardened context breaks a root image at start.
3. **Namespace label**: `pod-security.kubernetes.io/enforce: restricted` only
   when every pod in the namespace qualifies. Today these do not, and they
   are outside this repository:
   - the GraphCommerce NFS cache server and the Magento PVC-media NFS server
     (`itsthenetwork/nfs-server-alpine`, privileged; plan item G8 hardens it,
     or it moves out of the tenant namespace);
   - dev mode's sshd and seed-source containers (`dev.go`, `runAsUser: 0`):
     a dev-mode namespace stays on `baseline`;
   - the data services that step down from root (MariaDB/Percona, see the
     note in `internal/podsec`), Varnish and the exporters: check each image
     before the namespace enforces.
   Until then, keep `enforce: baseline` + `warn/audit: restricted`; the
   recipe images no longer produce the warnings.
4. **Dev mode** mounts source into the php container and runs sshd as root in
   a side container. That needs its own decision (a dev namespace on
   `baseline` is the conservative answer).
