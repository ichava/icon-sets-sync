<?php

declare(strict_types=1);

use Simtabi\Laranail\Ichava\IconSetsSync\Checksums;
use Simtabi\Laranail\Ichava\IconSetsSync\Installer;
use Simtabi\Laranail\Ichava\IconSetsSync\Launcher;
use Simtabi\Laranail\Ichava\IconSetsSync\LauncherException;
use Simtabi\Laranail\Ichava\IconSetsSync\Platform;
use Simtabi\Laranail\Ichava\IconSetsSync\Tests\FakeFetcher;

beforeEach(function (): void {
    $this->cache = sys_get_temp_dir() . '/icon-sets-sync-test-' . bin2hex(random_bytes(6));
    $this->binary = random_bytes(64);
    $this->asset = 'icon-sets-sync_linux_amd64';
    $this->base = 'https://github.com/ichava/icon-sets-sync/releases/download/v0.1.2/';
});

afterEach(function (): void {
    foreach (glob($this->cache . '/*/*') ?: [] as $f) {
        unlink($f);
    }
    foreach (glob($this->cache . '/*') ?: [] as $d) {
        rmdir($d);
    }
    is_dir($this->cache) && rmdir($this->cache);
});

function installer(object $test, FakeFetcher $fetcher, bool $requireSignature = false): Installer
{
    return new Installer($fetcher, new Platform('linux', 'amd64'), $test->cache, 'v0.1.2', $requireSignature);
}

it('installs a binary whose checksum matches, executable, and reuses it', function (): void {
    $fetcher = new FakeFetcher([
        $this->base . 'checksums.txt' => hash('sha256', $this->binary) . "  {$this->asset}\n",
        $this->base . $this->asset => $this->binary,
    ]);

    $path = installer($this, $fetcher)->ensure();

    expect(file_get_contents($path))->toBe($this->binary)
        ->and(is_executable($path))->toBeTrue()
        ->and($path)->toEndWith('/v0.1.2/' . $this->asset);

    installer($this, $fetcher)->ensure();
    expect($fetcher->requested)->toHaveCount(2); // the second call hit the cache
});

it('refuses a binary whose checksum does not match and installs nothing', function (): void {
    $fetcher = new FakeFetcher([
        $this->base . 'checksums.txt' => hash('sha256', 'something else') . "  {$this->asset}\n",
        $this->base . $this->asset => $this->binary,
    ]);
    $installer = installer($this, $fetcher);

    expect(fn () => $installer->ensure())->toThrow(LauncherException::class, 'Checksum mismatch');
    expect(file_exists($installer->path()))->toBeFalse();
});

it('refuses a binary checksums.txt does not list', function (): void {
    Checksums::parse(str_repeat('a', 64) . "  icon-sets-sync_linux_arm64\n")->verify('icon-sets-sync_linux_amd64', 'x');
})->throws(LauncherException::class, 'does not list');

it('refuses an empty checksums file', function (): void {
    Checksums::parse("<html>not found</html>\n");
})->throws(LauncherException::class, 'names no files');

it('fails closed when a signature is required and cosign is absent', function (): void {
    $fetcher = new FakeFetcher([$this->base . 'checksums.txt' => hash('sha256', $this->binary) . "  {$this->asset}\n"]);

    installer($this, $fetcher, requireSignature: true)->ensure();
})->throws(LauncherException::class, 'cosign is not on PATH');

it('builds tag and latest download URLs', function (): void {
    $fetcher = new FakeFetcher([]);
    $latest = new Installer($fetcher, new Platform('linux', 'amd64'), $this->cache);

    expect(installer($this, $fetcher)->url('checksums.txt'))->toBe($this->base . 'checksums.txt')
        ->and($latest->url('checksums.txt'))
        ->toBe('https://github.com/ichava/icon-sets-sync/releases/latest/download/checksums.txt');
});

it('pins the release to the installed tag, or the latest for a branch install', function (?string $override, ?string $installed, ?string $tag): void {
    expect(Launcher::tag($override, $installed))->toBe($tag);
})->with([
    'tag install' => [null, 'v0.1.3', 'v0.1.3'],
    'bare version' => [null, '0.1.3', 'v0.1.3'],
    'branch install' => [null, 'dev-main', null],
    'override wins' => ['0.1.2', 'v0.1.3', 'v0.1.2'],
]);
