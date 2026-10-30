<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync;

/**
 * The OS and architecture tokens the release assets are named with.
 *
 * GoReleaser names every bare binary `icon-sets-sync_{os}_{arch}`, with
 * `macos` for darwin, `armv6`/`armv7` for 32-bit ARM and `.exe` on Windows
 * (src/.goreleaser.yaml). This maps PHP's view of the host onto those tokens
 * and refuses a host the release matrix does not build for.
 */
final readonly class Platform
{
    /** @var array<string, list<string>> the os => arch pairs the release ships */
    private const SHIPPED = [
        'linux' => ['amd64', 'arm64', 'armv6', 'armv7', '386', 'riscv64', 'ppc64le', 's390x'],
        'macos' => ['amd64', 'arm64'],
        'windows' => ['amd64', 'arm64', '386'],
        'freebsd' => ['amd64', 'arm64', 'armv6', 'armv7', '386'],
    ];

    private const OS = [
        'Linux' => 'linux',
        'Darwin' => 'macos',
        'Windows' => 'windows',
        'BSD' => 'freebsd',
    ];

    private const ARCH = [
        'x86_64' => 'amd64',
        'amd64' => 'amd64',
        'x64' => 'amd64',
        'arm64' => 'arm64',
        'aarch64' => 'arm64',
        'armv8l' => 'arm64',
        'i386' => '386',
        'i486' => '386',
        'i586' => '386',
        'i686' => '386',
        'x86' => '386',
        'armv6l' => 'armv6',
        'armv7l' => 'armv7',
        'armv7' => 'armv7',
        'riscv64' => 'riscv64',
        'ppc64le' => 'ppc64le',
        's390x' => 's390x',
    ];

    public function __construct(public string $os, public string $arch)
    {
        if (! in_array($arch, self::SHIPPED[$os] ?? [], true)) {
            throw new LauncherException(sprintf(
                'No icon-sets-sync release binary is built for %s/%s. Build it from source: '
                . 'go -C src build ./cmd/icon-sets-sync (https://github.com/ichava/icon-sets-sync).',
                $os,
                $arch,
            ));
        }
    }

    /**
     * @param  string  $osFamily  PHP_OS_FAMILY
     * @param  string  $machine  php_uname('m')
     */
    public static function detect(string $osFamily = PHP_OS_FAMILY, ?string $machine = null): self
    {
        $machine = strtolower(trim($machine ?? php_uname('m')));
        $os = self::OS[$osFamily] ?? strtolower($osFamily);

        return new self($os, self::ARCH[$machine] ?? $machine);
    }

    public function assetName(string $binary = 'icon-sets-sync'): string
    {
        return sprintf('%s_%s_%s%s', $binary, $this->os, $this->arch, $this->os === 'windows' ? '.exe' : '');
    }
}
