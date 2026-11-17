# shellcheck shell=sh
# Sourced by each build step that runs PHP or composer. Exports
# PHP_EXTENSIONS (php-extensions.php), then the PHP_* variables and
# COMPOSER_VERSION of the BuildKit secret build-env, and selects that
# Composer version. Only single-line assignments are evaluated. PHP_VERSION
# is the image's.
PHP_EXTENSIONS=$(cat /tmp/php-extensions)
export PHP_EXTENSIONS
if [ -f /run/secrets/build-env ]; then
  set -a
  eval "$(sed -nE -e '/^PHP_VERSION=/d' -e "\#^(PHP_[A-Z0-9_]*|COMPOSER_VERSION)=('[^']*'|[A-Za-z0-9_@%+=:,./-]*)\$#p" /run/secrets/build-env)"
  set +a
fi
if [ -n "${COMPOSER_VERSION:-}" ]; then
  sudo /usr/local/bin/select_composer_version.sh "$COMPOSER_VERSION"
fi
