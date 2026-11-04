<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync;

use Composer\InstalledVersions;
use Throwable;

/**
 * What vendor/bin/icon-sets-sync runs: work out which release this install
 * corresponds to, make sure its binary is in the cache, and hand over.
 */
final class Launcher
{
    /** @param list<string> $arguments the user's arguments, without the script name */
    public static function main(array $arguments): int
    {
        try {
            return self::exec(self::binary(), $arguments);
        } catch (Throwable $e) {
            fwrite(STDERR, 'icon-sets-sync: ' . $e->getMessage() . PHP_EOL);
            fwrite(STDERR, 'icon-sets-sync: build from source '
                . '(git clone https://github.com/ichava/icon-sets-sync.git && make -C icon-sets-sync build-local) '
                . 'and set ICHAVA_ICON_SETS_SYNC_BIN to the built binary.' . PHP_EOL);

            return 1;
        }
    }

    /**
     * The binary to run: the one ICHAVA_ICON_SETS_SYNC_BIN names, or, only
     * when ICHAVA_ICON_SETS_SYNC_DOWNLOAD opts in, a verified release binary.
     * Go releases are git only until the owner says otherwise, so the
     * download is never the default (G-06).
     *
     * @throws LauncherException when neither is available
     */
    public static function binary(): string
    {
        $local = self::localBinary();
        if ($local !== null) {
            return $local;
        }

        if (! self::downloadAllowed()) {
            throw new LauncherException('no binary is named, and downloading a release needs ICHAVA_ICON_SETS_SYNC_DOWNLOAD=1.');
        }

        $installer = new Installer(
            fetcher: new HttpFetcher,
            platform: Platform::detect(),
            cacheDir: dirname(__DIR__, 2) . DIRECTORY_SEPARATOR . '.cache' . DIRECTORY_SEPARATOR . 'bin',
            tag: self::tag(),
            requireSignature: in_array(getenv('ICHAVA_ICON_SETS_SYNC_REQUIRE_SIGNATURE'), ['1', 'true'], true),
            cosign: self::which('cosign'),
        );

        return $installer->ensure();
    }

    /** Whether ICHAVA_ICON_SETS_SYNC_DOWNLOAD (or $value) opts in to the release download. */
    public static function downloadAllowed(?string $value = null): bool
    {
        $value ??= getenv('ICHAVA_ICON_SETS_SYNC_DOWNLOAD') ?: null;

        return in_array($value, ['1', 'true'], true);
    }

    /**
     * A binary the user built from source, named by ICHAVA_ICON_SETS_SYNC_BIN.
     * Used instead of a downloaded release; no release binaries are published yet.
     */
    public static function localBinary(?string $path = null): ?string
    {
        $path ??= getenv('ICHAVA_ICON_SETS_SYNC_BIN') ?: null;
        if ($path === null) {
            return null;
        }
        if (! is_file($path) || ! is_executable($path)) {
            throw new LauncherException("ICHAVA_ICON_SETS_SYNC_BIN is set to {$path}, which is not an executable file.");
        }

        return $path;
    }

    /**
     * The release tag to fetch: an explicit override, else the version Composer
     * installed when that is a tag, else null for the latest release.
     */
    public static function tag(?string $override = null, ?string $installed = null): ?string
    {
        $override ??= getenv('ICHAVA_ICON_SETS_SYNC_VERSION') ?: null;
        if ($override !== null) {
            return str_starts_with($override, 'v') ? $override : "v{$override}";
        }

        if ($installed === null && class_exists(InstalledVersions::class)
            && InstalledVersions::isInstalled(Installer::REPOSITORY)) {
            $installed = InstalledVersions::getPrettyVersion(Installer::REPOSITORY);
        }

        return $installed !== null && preg_match('/^v?\d+\.\d+\.\d+$/', $installed) === 1
            ? (str_starts_with($installed, 'v') ? $installed : "v{$installed}")
            : null;
    }

    /** @param list<string> $arguments */
    private static function exec(string $binary, array $arguments): int
    {
        $process = proc_open([$binary, ...$arguments], [STDIN, STDOUT, STDERR], $pipes);
        if (! is_resource($process)) {
            throw new LauncherException("Could not start {$binary}.");
        }

        return proc_close($process);
    }

    private static function which(string $command): ?string
    {
        foreach (explode(PATH_SEPARATOR, (string) getenv('PATH')) as $dir) {
            foreach (PHP_OS_FAMILY === 'Windows' ? ["{$command}.exe", $command] : [$command] as $name) {
                $candidate = $dir . DIRECTORY_SEPARATOR . $name;
                if ($dir !== '' && is_file($candidate) && is_executable($candidate)) {
                    return $candidate;
                }
            }
        }

        return null;
    }
}
