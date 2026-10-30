<?php

declare(strict_types=1);

use Simtabi\Laranail\Ichava\IconSetsSync\LauncherException;
use Simtabi\Laranail\Ichava\IconSetsSync\Platform;

it('maps the host onto the release asset name', function (string $family, string $machine, string $asset): void {
    expect(Platform::detect($family, $machine)->assetName())->toBe($asset);
})->with([
    'linux x86_64' => ['Linux', 'x86_64', 'icon-sets-sync_linux_amd64'],
    'linux aarch64' => ['Linux', 'aarch64', 'icon-sets-sync_linux_arm64'],
    'linux armv7l' => ['Linux', 'armv7l', 'icon-sets-sync_linux_armv7'],
    'linux i686' => ['Linux', 'i686', 'icon-sets-sync_linux_386'],
    'macos arm64' => ['Darwin', 'arm64', 'icon-sets-sync_macos_arm64'],
    'macos x86_64' => ['Darwin', 'x86_64', 'icon-sets-sync_macos_amd64'],
    'windows AMD64' => ['Windows', 'AMD64', 'icon-sets-sync_windows_amd64.exe'],
    'freebsd amd64' => ['BSD', 'amd64', 'icon-sets-sync_freebsd_amd64'],
]);

it('refuses a host the release matrix does not build', function (string $family, string $machine): void {
    Platform::detect($family, $machine);
})->with([
    'macos 386' => ['Darwin', 'i386'],
    'windows armv7' => ['Windows', 'armv7l'],
    'solaris' => ['Solaris', 'x86_64'],
    'unknown arch' => ['Linux', 'mips'],
])->throws(LauncherException::class, 'No icon-sets-sync release binary');
