<?php

declare(strict_types=1);

namespace Simtabi\Laranail\Ichava\IconSetsSync;

/**
 * Plain HTTPS through PHP's stream wrapper, following GitHub's redirect to the
 * release-asset host. No extension beyond openssl, so the launcher works on any
 * PHP that can run Composer. Honours HTTPS_PROXY the way Composer does, since
 * PHP's stream wrapper ignores it.
 */
final class HttpFetcher implements Fetcher
{
    public function fetch(string $url): string
    {
        $headers = ['User-Agent: ichava-icon-sets-sync-composer'];
        $http = ['follow_location' => 1, 'max_redirects' => 5, 'timeout' => 60, 'ignore_errors' => true];

        $proxy = getenv('https_proxy') ?: getenv('HTTPS_PROXY') ?: null;
        if (is_string($proxy) && ($parts = parse_url($proxy)) !== false && isset($parts['host'])) {
            $http['proxy'] = sprintf('tcp://%s:%d', $parts['host'], $parts['port'] ?? 80);
            if (isset($parts['user'])) {
                $headers[] = 'Proxy-Authorization: Basic ' . base64_encode(
                    rawurldecode($parts['user']) . ':' . rawurldecode($parts['pass'] ?? ''),
                );
            }
        }

        $context = stream_context_create(['http' => $http + ['header' => implode("\r\n", $headers) . "\r\n"]]);

        $body = @file_get_contents($url, false, $context);
        /** @var list<string> $response */
        $response = function_exists('http_get_last_response_headers')
            ? (http_get_last_response_headers() ?? [])
            : ($http_response_header ?? []);

        $status = 0;
        foreach ($response as $header) {
            if (preg_match('#^HTTP/\S+\s+(\d{3})#', $header, $m) === 1) {
                $status = (int) $m[1]; // the last status line is the final response
            }
        }

        if ($body === false || $status !== 200) {
            throw new LauncherException(sprintf('Could not download %s (HTTP %s).', $url, $status ?: 'no response'));
        }

        return $body;
    }
}
