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

if [ "$#" -gt 0 ]; then
  exec "$@"
fi
