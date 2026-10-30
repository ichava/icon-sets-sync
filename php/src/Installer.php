<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync;

/**
 * Puts the release binary for this host in a cache directory, verified, and
 * returns its path. Downloads only when the cached copy is missing.
 *
 * Verification is the checksums.txt the release publishes. That file is
 * signed keylessly through Sigstore; when `cosign` is on PATH the signature is
 * checked too, and ICHAVA_ICON_SETS_SYNC_REQUIRE_SIGNATURE=1 makes a missing
 * cosign an error rather than a skip.
 */
final readonly class Installer
{
    public const REPOSITORY = 'ichava/icon-sets-sync';

    private const SIGNER = '^https://github.com/ichava/icon-sets-sync/\.github/workflows/release\.yml@refs/tags/v';

    private const ISSUER = 'https://token.actions.githubusercontent.com';

    public function __construct(
        private Fetcher $fetcher,
        private Platform $platform,
        private string $cacheDir,
        private ?string $tag = null,
        private bool $requireSignature = false,
        private ?string $cosign = null,
    ) {}

    public function path(): string
    {
        return $this->cacheDir . DIRECTORY_SEPARATOR . ($this->tag ?? 'latest')
            . DIRECTORY_SEPARATOR . $this->platform->assetName();
    }

    public function ensure(): string
    {
        $target = $this->path();
        if (is_file($target)) {
            return $target;
        }

        $asset = $this->platform->assetName();
        $checksums = $this->fetcher->fetch($this->url('checksums.txt'));
        $this->verifySignature($checksums);

        $binary = $this->fetcher->fetch($this->url($asset));
        Checksums::parse($checksums)->verify($asset, $binary);

        $dir = dirname($target);
        if (! is_dir($dir) && ! @mkdir($dir, 0o755, true) && ! is_dir($dir)) {
            throw new LauncherException(sprintf('Could not create %s.', $dir));
        }

        $partial = $target . '.' . bin2hex(random_bytes(4)) . '.part';
        if (file_put_contents($partial, $binary) !== strlen($binary) || ! chmod($partial, 0o755) || ! rename($partial, $target)) {
            @unlink($partial);
            throw new LauncherException(sprintf('Could not write %s.', $target));
        }

        return $target;
    }

    public function url(string $file): string
    {
        return $this->tag === null
            ? sprintf('https://github.com/%s/releases/latest/download/%s', self::REPOSITORY, $file)
            : sprintf('https://github.com/%s/releases/download/%s/%s', self::REPOSITORY, rawurlencode($this->tag), $file);
    }

    private function verifySignature(string $checksums): void
    {
        if ($this->cosign === null) {
            if ($this->requireSignature) {
                throw new LauncherException('ICHAVA_ICON_SETS_SYNC_REQUIRE_SIGNATURE is set but cosign is not on PATH.');
            }

            return;
        }

        $work = sys_get_temp_dir() . DIRECTORY_SEPARATOR . 'icon-sets-sync-' . bin2hex(random_bytes(6));
        if (! @mkdir($work, 0o700)) {
            throw new LauncherException('Could not create a scratch directory for signature verification.');
        }

        try {
            file_put_contents("{$work}/checksums.txt", $checksums);
            file_put_contents("{$work}/checksums.txt.sigstore.json", $this->fetcher->fetch($this->url('checksums.txt.sigstore.json')));

            $process = proc_open([
                $this->cosign, 'verify-blob',
                '--bundle', "{$work}/checksums.txt.sigstore.json",
                '--certificate-identity-regexp', self::SIGNER,
                '--certificate-oidc-issuer', self::ISSUER,
                "{$work}/checksums.txt",
            ], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);

            if (! is_resource($process)) {
                throw new LauncherException('Could not start cosign.');
            }
            $output = stream_get_contents($pipes[1]) . stream_get_contents($pipes[2]);
            fclose($pipes[1]);
            fclose($pipes[2]);

            if (proc_close($process) !== 0) {
                throw new LauncherException("cosign rejected the signature on checksums.txt:\n" . trim((string) $output));
            }
        } finally {
            array_map('unlink', glob("{$work}/*") ?: []);
            @rmdir($work);
        }
    }
}
