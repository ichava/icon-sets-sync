<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync;

/**
 * The release's checksums.txt: `<sha256>  <bare file name>` per line.
 */
final readonly class Checksums
{
    /** @param array<string, string> $hashes file name => lowercase sha256 */
    private function __construct(private array $hashes) {}

    public static function parse(string $contents): self
    {
        $hashes = [];

        foreach (preg_split('/\R/', $contents) ?: [] as $line) {
            if (preg_match('/^([0-9a-fA-F]{64})\s+\*?(\S+)$/', trim($line), $m) === 1) {
                $hashes[$m[2]] = strtolower($m[1]);
            }
        }

        if ($hashes === []) {
            throw new LauncherException('checksums.txt names no files; refusing to run an unverified binary.');
        }

        return new self($hashes);
    }

    public function verify(string $asset, string $contents): void
    {
        $expected = $this->hashes[$asset]
            ?? throw new LauncherException(sprintf('checksums.txt does not list %s; refusing to run it.', $asset));

        if (! hash_equals($expected, hash('sha256', $contents))) {
            throw new LauncherException(sprintf(
                'Checksum mismatch for %s: the download does not match checksums.txt. Nothing was installed.',
                $asset,
            ));
        }
    }
}
