. (Join-Path $PSScriptRoot 'Common.ps1')
if (-not (Test-Path -LiteralPath (Join-Path $script:WalletRoot '.env'))) { throw 'Missing .env; no unrelated service was changed.' }
Invoke-WalletCompose -ComposeArguments @('stop')
Write-Output 'WalletLedger stopped. Its database volume remains available.'
