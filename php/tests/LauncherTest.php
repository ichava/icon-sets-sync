<?php

declare(strict_types=1);

use Simtabi\Laranail\Ichava\IconSetsSync\Launcher;
use Simtabi\Laranail\Ichava\IconSetsSync\LauncherException;

it('uses a binary built from source when one is named', function (): void {
    $bin = tempnam(sys_get_temp_dir(), 'iss');
    chmod($bin, 0755);

    expect(Launcher::localBinary($bin))->toBe($bin);

    unlink($bin);
});

it('refuses a named binary that is not executable', function (): void {
    Launcher::localBinary(sys_get_temp_dir() . '/icon-sets-sync-missing-' . bin2hex(random_bytes(4)));
})->throws(LauncherException::class, 'not an executable file');

it('falls back to the release download when no binary is named', function (): void {
    putenv('ICHAVA_ICON_SETS_SYNC_BIN');

    expect(Launcher::localBinary())->toBeNull();
});
