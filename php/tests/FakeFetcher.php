<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync\Tests;

use Simtabi\Laranail\Ichava\IconSetsSync\Fetcher;
use Simtabi\Laranail\Ichava\IconSetsSync\LauncherException;

final class FakeFetcher implements Fetcher
{
    /** @var list<string> */
    public array $requested = [];

    /** @param array<string, string> $responses url => body */
    public function __construct(private readonly array $responses) {}

    public function fetch(string $url): string
    {
        $this->requested[] = $url;

        return $this->responses[$url] ?? throw new LauncherException("unexpected fetch {$url}");
    }
}
