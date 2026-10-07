<?php
/**
 * PLATFORM BUILD CHECK: every patch file in patches/ is actually applied.
 *
 * Run by the recipe Dockerfile right after `composer install`, from the
 * project root (php .platform/check-patches.php). It fails the build, naming
 * the file, when a *.patch under patches/ did not reach the installed code.
 *
 * Why: vaimo/composer-patches can skip a patch without failing. A patch only
 * found by the `extra.patches-search` folder scan is silently dropped when
 *   - its header names no target package (@package), or names one that is not
 *     installed (e.g. magento/* on Mage-OS, where the code is mage-os/*), or
 *   - the target is a branch install (dev-main, a VCS package): the scan
 *     gives every patch an implicit `@version >=0.0.0`, which a branch
 *     version never satisfies.
 * The build then says "Nothing to patch", stays green, and ships an
 * unpatched image (deployyy-operator#276). vaimo's own bookkeeping
 * (`composer patch:list`, `patches_applied` in installed.json) is not proof
 * either: it reported a patched branch install as NEW. So this check looks at
 * the code on disk: a patch is applied when it reverses cleanly
 * (`patch -R --dry-run`) in its target package's install directory.
 *
 * Target per file: its `extra.patches` declaration (composer.json, or a
 * `patches-file` it names) wins; otherwise the vaimo header (@package, or its
 * aliases @target/@module/@targets). A file that is in patches/ on purpose but
 * must not be applied carries `@skip` in its header (vaimo's own flag); vaimo's
 * dev-only patches (`@type dev`) are skipped too, the build installs --no-dev.
 */

$root = getcwd();
$patchDir = $root . '/patches';

if (!is_dir($patchDir)) {
    exit(0);
}

$files = [];
$it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($patchDir, FilesystemIterator::SKIP_DOTS));
foreach ($it as $f) {
    if ($f->isFile() && preg_match('/\.patch$/i', $f->getFilename())) {
        $files[] = substr($f->getPathname(), strlen($root) + 1);
    }
}
sort($files);

if (!$files) {
    exit(0);
}

$composer = json_decode((string) file_get_contents($root . '/composer.json'), true) ?: [];
$extra = $composer['extra'] ?? [];

// Explicit declarations: path => package.
$declared = [];
$collect = function ($patches) use (&$declared) {
    foreach ((array) $patches as $package => $defs) {
        foreach ((array) $defs as $label => $def) {
            $source = is_string($def) ? $def : ($def['source'] ?? $def['url'] ?? null);
            if (is_string($source) && !preg_match('#^[a-z]+://#i', $source)) {
                $declared[ltrim(preg_replace('#^\./#', '', $source), '/')] = $package;
            }
        }
    }
};
$collect($extra['patches'] ?? []);
foreach ((array) ($extra['patches-file'] ?? []) as $patchesFile) {
    $json = is_file($root . '/' . $patchesFile)
        ? json_decode((string) file_get_contents($root . '/' . $patchesFile), true)
        : null;
    $collect($json['patches'] ?? []);
}

// Installed packages: name => install directory.
$installed = json_decode((string) @file_get_contents($root . '/vendor/composer/installed.json'), true) ?: [];
$installDirs = [];
foreach ($installed['packages'] ?? $installed as $p) {
    if (isset($p['name'], $p['install-path'])) {
        $installDirs[$p['name']] = realpath($root . '/vendor/composer/' . $p['install-path']) ?: null;
    }
}
if (isset($composer['name'])) {
    $installDirs[$composer['name']] = $root;
}

// The same two tools vaimo applies with, in its order.
$patchBin = trim((string) shell_exec('command -v patch 2>/dev/null'));
$gitBin = trim((string) shell_exec('command -v git 2>/dev/null'));
if ($patchBin === '' && $gitBin === '') {
    fwrite(STDERR, "FATAL: check-patches needs `patch` or `git` to verify patches/, and neither is installed\n");
    exit(1);
}

$failures = [];
foreach ($files as $file) {
    $contents = str_replace("\r\n", "\n", (string) file_get_contents($root . '/' . $file));

    // vaimo's header: everything before the first ---/+++ line.
    $tags = [];
    foreach (explode("\n", $contents) as $line) {
        if (strncmp($line, '--- ', 4) === 0 || strncmp($line, '+++ ', 4) === 0) {
            break;
        }
        if (preg_match('/^@([a-z-]+)\s*(.*)$/i', $line, $m)) {
            $tags[strtolower($m[1])] = trim($m[2]);
        }
    }

    if (array_key_exists('skip', $tags)) {
        echo "patch $file: @skip, not checked\n";
        continue;
    }
    if (isset($tags['type']) && array_intersect(explode(',', strtolower($tags['type'])), ['dev', 'developer', 'development', 'develop'])) {
        echo "patch $file: dev-only, not checked (the build installs --no-dev)\n";
        continue;
    }

    $package = $declared[$file] ?? null;
    if ($package === null) {
        foreach (['package', 'target', 'module', 'targets'] as $tag) {
            if (!empty($tags[$tag])) {
                $package = trim(explode(':', $tags[$tag])[0]);
                break;
            }
        }
    }
    if ($package === null || $package === '') {
        $failures[] = "$file: names no target package — declare it in extra.patches of composer.json";
        continue;
    }

    $dir = $package === '*' ? realpath($root . '/vendor') : ($installDirs[$package] ?? null);
    if (!$dir) {
        $failures[] = "$file: targets $package, which is not installed (on Mage-OS the core packages are mage-os/*, not magento/*)";
        continue;
    }

    // Applied = reverses cleanly. -f, not -t: -t would treat a not-yet-applied
    // patch as "reversed" and dry-run it forward, which also exits 0.
    $applied = false;
    foreach ([1, 0, 2] as $level) {
        $cmd = $patchBin !== ''
            ? sprintf('%s -R -f -s --dry-run -p%d -i %s', escapeshellarg($patchBin), $level, escapeshellarg($root . '/' . $file))
            : sprintf('%s apply -R --check -p%d %s', escapeshellarg($gitBin), $level, escapeshellarg($root . '/' . $file));
        exec(sprintf('cd %s && %s >/dev/null 2>&1', escapeshellarg($dir), $cmd), $out, $code);
        if ($code === 0) {
            $applied = true;
            break;
        }
    }

    if ($applied) {
        echo "patch $file: applied to $package\n";
    } else {
        $failures[] = isset($declared[$file])
            ? "$file: declared for $package but NOT applied (no longer matches the installed version?)"
            : "$file: NOT applied to $package — not declared in extra.patches, so only the patches-search scan could find it, and that skips a branch install (dev-main) unless the header has @version *";
    }
}

if ($failures) {
    fwrite(STDERR, "FATAL: patches in patches/ that are not in the installed code:\n");
    foreach ($failures as $failure) {
        fwrite(STDERR, "  - $failure\n");
    }
    fwrite(STDERR, "Declare each patch in extra.patches of composer.json (package => {label: path}),\n"
        . "or mark a file kept there on purpose with @skip in its header.\n");
    exit(1);
}
