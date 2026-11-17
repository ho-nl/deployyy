<?php
// Writes to the file $argv[1] the PHP extensions that composer.json and
// composer.lock in the current directory require, that the image has and that
// are off: the value of PHP_EXTENSIONS in the build and in the image.
$require = array_keys(json_decode(@file_get_contents('composer.json') ?: '{}', true)['require'] ?? []);
foreach (json_decode(@file_get_contents('composer.lock') ?: '{}', true)['packages'] ?? [] as $package) {
    $require = array_merge($require, array_keys($package['require'] ?? []));
}
$loaded = array_map(fn ($n) => strtolower(str_replace('Zend ', '', $n)), array_merge(get_loaded_extensions(), get_loaded_extensions(true)));
$off = [];
foreach ($require as $name) {
    if (!preg_match('/^ext-([a-z0-9_-]+)$/i', $name, $m)) {
        continue;
    }
    $extension = strtolower($m[1]) === 'zend-opcache' ? 'opcache' : strtolower($m[1]);
    if (is_file('/etc/php/' . getenv('PHP_VERSION') . "/mods-available/$extension.ini") && !in_array($extension, $loaded, true)) {
        $off[$extension] = $extension;
    }
}
sort($off);
fwrite(STDERR, 'PHP extensions turned on for composer.json: ' . ($off ? implode(' ', $off) : 'none') . "\n");
file_put_contents($argv[1], implode(' ', $off) . "\n");
