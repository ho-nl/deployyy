#!/bin/bash
# Sudo-less /usr/bin/php for the Magento php-fpm image (recipes/magento, recipes/mageos-3).
#
# thecodingmachine's wrapper rebuilds the PHP config when PHP_INI_* /
# PHP_EXTENSION_* variables changed since the last run, through `sudo`. A
# Job or `kubectl exec` that bypasses the entrypoint still gets its own
# variables applied; here without sudo (the Dockerfile made the config
# files owned by the app user). See php-entrypoint.sh.
# shellcheck source=/dev/null
source /usr/local/bin/php_env_fingerprint.sh
FINGERPRINT=$(php_env_fingerprint)
read -r GENERATED_FINGERPRINT < /opt/php_env_fingerprint || GENERATED_FINGERPRINT=""

if [[ "$FINGERPRINT" != "$GENERATED_FINGERPRINT" ]]; then
  /usr/bin/real_php -d display_errors=stderr /usr/local/bin/generate_conf.php > "/etc/php/${PHP_VERSION}/mods-available/generated_conf.ini"
  PHP_VERSION="${PHP_VERSION}" /usr/bin/real_php -d display_errors=stderr /usr/local/bin/setup_extensions.php | bash
  echo "$FINGERPRINT" > /opt/php_env_fingerprint
fi

exec /usr/bin/real_php "$@"
