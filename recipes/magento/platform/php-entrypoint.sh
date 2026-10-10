#!/bin/bash
# Sudo-less entrypoint for the Magento php-fpm image (recipes/magento).
#
# Replaces thecodingmachine's docker-entrypoint.sh, which re-execs itself
# through `sudo` and so cannot run with allowPrivilegeEscalation: false (Pod
# Security Standard `restricted`). It does the parts of that script the
# platform uses, as the app user:
#   - write PHP config from PHP_INI_* variables (generated_conf.ini) and
#     enable/disable extensions from PHP_EXTENSION_* variables — the
#     Dockerfile made those files owned by the app user;
#   - run STARTUP_COMMAND_* variables.
# Left out: the uid remapping and host detection for local Docker Desktop
# use, and the CRON_* supercronic launcher (the platform runs supercronic
# as its own container).
#
# The Dockerfile runs this once at build time with no arguments, so the
# image starts with its config already written; at start it runs again and
# picks up the pod's own PHP_INI_* variables.
set -euo pipefail

real_php=/usr/bin/real_php
"$real_php" /usr/local/bin/generate_conf.php > "/etc/php/${PHP_VERSION}/mods-available/generated_conf.ini"
PHP_VERSION="${PHP_VERSION}" "$real_php" /usr/local/bin/setup_extensions.php | bash
# Record the variables this config was built from, so the `php` wrapper
# does not rebuild it on every CLI call.
"$real_php" /usr/local/bin/check_php_env_var_changes.php > /dev/null
"$real_php" /usr/local/bin/startup_commands.php | bash

# FastBoot (graphcommerce/magento-fast-boot), when the project installs it, for the
# php-fpm master only. fastboot:prepare fills the node-local caches before the first
# request. Preload loads the classes of the project's committed preload-classes.txt
# into OPcache when the master starts; without that file it loads nothing. The image
# never changes, so the next start is the only reload. A dev box checks file times
# because its code changes, so it gets no preload.
preload=/var/www/html/vendor/graphcommerce/magento-fast-boot/src/FastBootPreload/preload.php
if [ "${1:-}" = "php-fpm" ] && [ -f "$preload" ]; then
  if [ -f /var/www/html/preload-classes.txt ]; then
    { mkdir -p /var/www/html/var/cache/preload \
      && install -m 0600 /var/www/html/preload-classes.txt /var/www/html/var/cache/preload/classes.txt \
      && rm -f /var/www/html/var/cache/preload/classes.recording; } \
      || echo "preload-classes.txt not installed; PHP-FPM preloads nothing" >&2
  fi
  timeout 120 php /var/www/html/bin/magento fastboot:prepare \
    || echo "fastboot:prepare failed; FastBoot fills its caches on the first requests" >&2
  if [ "${PHP_INI_OPCACHE__VALIDATE_TIMESTAMPS:-0}" != "1" ]; then
    set -- "$@" -d "opcache.preload=$preload"
  fi
fi

if [ "$#" -gt 0 ]; then
  exec "$@"
fi
