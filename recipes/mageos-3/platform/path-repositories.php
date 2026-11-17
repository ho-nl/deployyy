<?php
// Copies the directories of the composer.json path repositories below $argv[1]
// to the same paths below $argv[2]. The dependency step of the build installs
// them before the rest of the code is copied.
[, $source, $target] = $argv;
if (!is_dir($target) && !mkdir($target, 0777, true)) {
    exit(1);
}
$composer = json_decode((string) @file_get_contents("$source/composer.json"), true);
foreach (is_array($composer) ? ($composer['repositories'] ?? []) : [] as $repository) {
    $url = is_array($repository) && ($repository['type'] ?? '') === 'path' ? ($repository['url'] ?? null) : null;
    if (!is_string($url) || $url === '') {
        continue;
    }
    if ($url[0] === '/' || str_contains($url, '..')) {
        echo "Path repository $url is outside the application directory: the build has no copy of it\n";
        continue;
    }
    foreach (glob("$source/$url", GLOB_ONLYDIR) ?: [] as $dir) {
        $relative = substr($dir, strlen($source) + 1);
        passthru('mkdir -p ' . escapeshellarg(dirname("$target/$relative")) . ' && cp -a ' . escapeshellarg($dir) . ' ' . escapeshellarg("$target/$relative"), $code);
        if ($code !== 0) {
            exit($code);
        }
        echo "Path repository for the dependency step: $relative\n";
    }
}
