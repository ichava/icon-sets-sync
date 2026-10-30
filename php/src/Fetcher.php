<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync;

interface Fetcher
{
    /** @throws LauncherException when the URL cannot be read */
    public function fetch(string $url): string;
}
