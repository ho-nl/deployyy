<?php
// Disables Magento's configuration check: the plugin `configHash`
// (Magento\Deploy\Model\Plugin\ConfigChangeDetector, declared on
// Magento\Framework\App\FrontControllerInterface in the global area of
// Magento_Deploy's etc/di.xml) that answers every request with "The
// configuration file has changed" while app/etc's configuration hash differs
// from the database's. The image is labelled app.deployyy.config-check=off;
// see the README, "Configuration check".
//
//   php config-check-off.php install  before setup:di:compile, in the project root
//   php config-check-off.php verify   after it: exits 1 while the plugin runs in any area

const MODULE = 'Deployyy_ConfigCheckOff';
const MODULE_DIR = 'app/code/Deployyy/ConfigCheckOff';
const PLUGIN = 'configHash';

function fail(string $message): never
{
    fwrite(STDERR, "config check: $message\n");
    exit(1);
}

function install(): void
{
    $files = [
        'registration.php' => <<<'PHP'
            <?php
            // Added by the deployyy platform build (.platform/config-check-off.php).
            \Magento\Framework\Component\ComponentRegistrar::register(
                \Magento\Framework\Component\ComponentRegistrar::MODULE,
                'Deployyy_ConfigCheckOff',
                __DIR__
            );

            PHP,
        'etc/module.xml' => <<<'XML'
            <?xml version="1.0"?>
            <!-- Added by the deployyy platform build (.platform/config-check-off.php). -->
            <config xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
                    xsi:noNamespaceSchemaLocation="urn:magento:framework:Module/etc/module.xsd">
                <module name="Deployyy_ConfigCheckOff">
                    <sequence>
                        <module name="Magento_Deploy"/>
                    </sequence>
                </module>
            </config>

            XML,
        // Global area: Magento_Deploy declares the plugin there only, and the
        // frontend, adminhtml, graphql and webapi areas inherit it.
        'etc/di.xml' => <<<'XML'
            <?xml version="1.0"?>
            <!-- Added by the deployyy platform build (.platform/config-check-off.php). -->
            <config xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
                    xsi:noNamespaceSchemaLocation="urn:magento:framework:ObjectManager/etc/config.xsd">
                <type name="Magento\Framework\App\FrontControllerInterface">
                    <plugin name="configHash" disabled="true"/>
                </type>
            </config>

            XML,
    ];
    foreach ($files as $name => $content) {
        $path = MODULE_DIR . '/' . $name;
        if (!is_dir(dirname($path)) && !mkdir(dirname($path), 0755, true)) {
            fail("cannot create " . dirname($path));
        }
        file_put_contents($path, $content);
    }

    // Enabled in config.php, so the project's composer.lock is not touched. An
    // explicit 0 there stays, and verify fails the build.
    $config = 'app/etc/config.php';
    $c = is_file($config) ? include $config : [];
    if (!isset($c['modules'])) {
        fail("$config has no module list");
    }
    if (array_key_exists(MODULE, $c['modules'])) {
        fwrite(STDERR, MODULE . ": kept at {$c['modules'][MODULE]} from config.php\n");
        return;
    }
    $c['modules'][MODULE] = 1;
    file_put_contents($config, "<?php\nreturn " . var_export($c, true) . ";\n");
    fwrite(STDERR, MODULE . ": enabled\n");
}

function verify(string $root): void
{
    // setup:di:compile writes each area's plugin list to
    // generated/metadata/<sorted scopes, |-separated>|plugin-list.php as
    // [plugins, inherited, processed]; `processed` names the plugins each
    // method runs.
    $files = glob("$root/generated/metadata/*plugin-list.php") ?: [];
    $areas = [];
    foreach ($files as $file) {
        $areas[] = basename($file);
        [, , $processed] = include $file;
        foreach ($processed as $call => $listeners) {
            foreach ($listeners as $names) {
                if (in_array(PLUGIN, (array) $names, true)) {
                    fail("plugin " . PLUGIN . " runs on $call in " . basename($file) . ". Is " . MODULE . " enabled in app/etc/config.php?");
                }
            }
        }
    }
    foreach (['frontend', 'graphql', 'webapi_rest', 'adminhtml'] as $area) {
        if (!preg_grep("/^(.+\\|)?$area\\|.*plugin-list\\.php$/", $areas)) {
            fail("no compiled plugin list for the $area area in $root/generated/metadata");
        }
    }
    echo "config check: plugin " . PLUGIN . " disabled in " . count($files) . " areas\n";
}

match ($argv[1] ?? '') {
    'install' => install(),
    'verify' => verify($argv[2] ?? getcwd()),
    default => fail("usage: config-check-off.php install|verify [root]"),
};
