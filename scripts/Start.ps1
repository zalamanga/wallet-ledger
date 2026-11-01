[CmdletBinding()]
param([switch]$NoBuild)
. (Join-Path $PSScriptRoot 'Common.ps1')
$engine = & docker info --format '{{.OSType}}'
if ($LASTEXITCODE -ne 0 -or $engine -ne 'linux') { throw 'Docker Desktop Linux containers are required.' }
Initialize-WalletEnvironment
$arguments = @('up','-d','--wait','--wait-timeout','240')
if (-not $NoBuild) { $arguments += '--build' }
Invoke-WalletCompose -ComposeArguments $arguments
$health = Invoke-RestMethod 'http://127.0.0.1:8170/api/health' -TimeoutSec 10
if ($health.status -ne 'ready') { throw 'WalletLedger readiness failed.' }
Write-Output 'WalletLedger: http://127.0.0.1:8170'
Write-Output 'Choose manual setup, or run the synthetic seed BEFORE filling setup: python scripts/seed-demo.py --confirm-synthetic'
Write-Output 'Stop: scripts/Stop.ps1. Volumes and credentials are retained.'
