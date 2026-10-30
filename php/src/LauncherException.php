<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync;

use RuntimeException;

/**
 * Anything that stops the launcher from handing over to the binary: an
 * unsupported platform, a failed download, or a checksum or signature that
 * does not match.
 */
final class LauncherException extends RuntimeException {}
