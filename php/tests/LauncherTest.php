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

// G-06: Go releases are git only until the owner says otherwise, so the
// release download stays off unless a user opts in explicitly.
it('downloads a release binary only when the user opts in', function (?string $value, bool $allowed): void {
    expect(Launcher::downloadAllowed($value))->toBe($allowed);
})->with([
    'unset'    => [null, false],
    'empty'    => ['', false],
    'no'       => ['0', false],
    'opted in' => ['1', true],
    'true'     => ['true', true],
]);

it('refuses to download without the opt-in, and says how to build', function (): void {
    putenv('ICHAVA_ICON_SETS_SYNC_BIN');
    putenv('ICHAVA_ICON_SETS_SYNC_DOWNLOAD');

    expect(fn () => Launcher::binary())->toThrow(LauncherException::class, 'ICHAVA_ICON_SETS_SYNC_DOWNLOAD');
});
