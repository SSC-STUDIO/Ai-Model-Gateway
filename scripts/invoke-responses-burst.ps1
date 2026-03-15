param(
    [string]$BaseUrl = "http://127.0.0.1:18080",
    [string]$Path = "/v1/responses/compact",
    [string]$ApiKey = "sk-local-gateway",
    [string]$Model = "gpt-5.2-codex",
    [int]$Concurrency = 20,
    [int]$RequestsPerWorker = 1,
    [int]$LaunchIntervalMs = 200,
    [int]$MaxRetries = 6,
    [int]$RetryBackoffMs = 3000,
    [int]$RetryBackoffMaxMs = 30000,
    [int]$TimeoutSec = 30
)

$ErrorActionPreference = "Stop"

function Get-RetryDelayMs {
    param(
        [int]$BaseMs,
        [int]$MaxMs,
        [int]$Attempt
    )

    if ($BaseMs -le 0 -or $Attempt -le 0) {
        return 0
    }
    if ($MaxMs -gt 0 -and $MaxMs -lt $BaseMs) {
        $MaxMs = $BaseMs
    }

    $delay = [int64]$BaseMs
    for ($i = 1; $i -lt $Attempt; $i++) {
        $delay *= 2
        if ($MaxMs -gt 0 -and $delay -ge $MaxMs) {
            $delay = $MaxMs
            break
        }
    }
    return [int]$delay
}

if ($Concurrency -le 0) {
    throw "Concurrency must be > 0"
}
if ($RequestsPerWorker -le 0) {
    throw "RequestsPerWorker must be > 0"
}

$uri = $BaseUrl.TrimEnd("/") + $Path
$headers = @{
    Authorization = "Bearer $ApiKey"
    "Content-Type" = "application/json"
    "User-Agent" = "Codex Burst/1.0"
}

$jobScript = {
    param($Uri, $Headers, $Model, $WorkerId, $RequestsPerWorker, $MaxRetries, $RetryBackoffMs, $RetryBackoffMaxMs, $TimeoutSec)

    function Get-RetryDelayMs {
        param([int]$BaseMs, [int]$MaxMs, [int]$Attempt)
        if ($BaseMs -le 0 -or $Attempt -le 0) {
            return 0
        }
        if ($MaxMs -gt 0 -and $MaxMs -lt $BaseMs) {
            $MaxMs = $BaseMs
        }
        $delay = [int64]$BaseMs
        for ($i = 1; $i -lt $Attempt; $i++) {
            $delay *= 2
            if ($MaxMs -gt 0 -and $delay -ge $MaxMs) {
                $delay = $MaxMs
                break
            }
        }
        return [int]$delay
    }

    $results = @()
    for ($requestIndex = 1; $requestIndex -le $RequestsPerWorker; $requestIndex++) {
        $requestId = "burst-$WorkerId-$requestIndex"
        $payload = @{
            model = $Model
            input = "burst-check $requestId"
        } | ConvertTo-Json -Depth 10

        $attempt = 0
        $succeeded = $false
        while (-not $succeeded) {
            $attempt++
            $startedAt = Get-Date
            try {
                $response = Invoke-WebRequest -UseBasicParsing -Uri $Uri -Method Post -Headers $Headers -Body $payload -TimeoutSec $TimeoutSec
                $results += [pscustomobject]@{
                    worker_id   = $WorkerId
                    request_id  = $requestId
                    attempt     = $attempt
                    status_code = [int]$response.StatusCode
                    success     = $true
                    duration_ms = [int]((Get-Date) - $startedAt).TotalMilliseconds
                    error       = ""
                }
                $succeeded = $true
            } catch {
                $message = $_.Exception.Message
                $statusCode = 0
                if ($_.Exception.Response -and $_.Exception.Response.StatusCode) {
                    $statusCode = [int]$_.Exception.Response.StatusCode
                }

                if ($attempt -gt $MaxRetries) {
                    $results += [pscustomobject]@{
                        worker_id   = $WorkerId
                        request_id  = $requestId
                        attempt     = $attempt
                        status_code = $statusCode
                        success     = $false
                        duration_ms = [int]((Get-Date) - $startedAt).TotalMilliseconds
                        error       = $message
                    }
                    break
                }

                $delayMs = Get-RetryDelayMs -BaseMs $RetryBackoffMs -MaxMs $RetryBackoffMaxMs -Attempt $attempt
                Start-Sleep -Milliseconds $delayMs
            }
        }
    }
    return $results
}

$jobs = @()
for ($workerId = 1; $workerId -le $Concurrency; $workerId++) {
    $jobs += Start-Job -ScriptBlock $jobScript -ArgumentList $uri, $headers, $Model, $workerId, $RequestsPerWorker, $MaxRetries, $RetryBackoffMs, $RetryBackoffMaxMs, $TimeoutSec
    if ($LaunchIntervalMs -gt 0 -and $workerId -lt $Concurrency) {
        Start-Sleep -Milliseconds $LaunchIntervalMs
    }
}

Wait-Job -Job $jobs | Out-Null
$results = $jobs | Receive-Job
$jobs | Remove-Job -Force | Out-Null

$total = @($results).Count
$success = @($results | Where-Object { $_.success }).Count
$failed = $total - $success
$avgMs = 0
if ($total -gt 0) {
    $avgMs = [int](($results | Measure-Object -Property duration_ms -Average).Average)
}

[pscustomobject]@{
    Uri               = $uri
    Concurrency       = $Concurrency
    RequestsPerWorker = $RequestsPerWorker
    TotalRequests     = $total
    Successes         = $success
    Failures          = $failed
    AverageDurationMs = $avgMs
    MaxRetries        = $MaxRetries
    RetryBackoffMs    = $RetryBackoffMs
    RetryBackoffMaxMs = $RetryBackoffMaxMs
} | Format-List

$results | Sort-Object worker_id, request_id, attempt | Format-Table -AutoSize
