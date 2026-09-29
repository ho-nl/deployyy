#!/bin/bash
# Sudo-less /usr/bin/php for the Magento php-fpm image (recipes/magento).
#
# thecodingmachine's wrapper rebuilds the PHP config when PHP_INI_* /
# PHP_EXTENSION_* variables changed since the last run, through `sudo`. A
# Job or `kubectl exec` that bypasses the entrypoint still gets its own
# variables applied; here without sudo (the Dockerfile made the config
# files owned by the app user). See php-entrypoint.sh.
REGENERATE=$(/usr/bin/real_php /usr/local/bin/check_php_env_var_changes.php)

if [[ "$REGENERATE" != "0" ]] && [[ "$REGENERATE" != "1" ]]; then
  >&2 echo "Unexpected PHP proxy output:"
  >&2 echo "$REGENERATE"
  exit 1
fi

if [[ "$REGENERATE" == "1" ]]; then
  /usr/bin/real_php /usr/local/bin/generate_conf.php > "/etc/php/${PHP_VERSION}/mods-available/generated_conf.ini"
  PHP_VERSION="${PHP_VERSION}" /usr/bin/real_php /usr/local/bin/setup_extensions.php | bash
fi

exec /usr/bin/real_php "$@"
