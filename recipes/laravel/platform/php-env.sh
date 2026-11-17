#!/bin/bash
# Runs a command with PHP_EXTENSIONS set to the extensions that composer.json
# and composer.lock require (php-extensions.php, written by the build). The
# image's entrypoint and its `php` (/usr/bin/php-image) run through this
# script, so every process and shell generates one PHP configuration.
PHP_EXTENSIONS="${PHP_EXTENSIONS:-$(</usr/local/lib/deployyy/php-extensions)}"
export PHP_EXTENSIONS
exec "$@"
