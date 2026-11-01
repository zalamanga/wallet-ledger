. (Join-Path $PSScriptRoot 'Common.ps1')
Import-WalletEnvironment
if (Get-NetTCPConnection -LocalPort 8171 -State Listen -ErrorAction SilentlyContinue) { throw 'Port8171 is occupied; no process was stopped.' }
$go = Get-WalletGo
$env:PORT='8171'; $env:WALLETLEDGER_HOST='127.0.0.1'
Push-Location (Join-Path $script:WalletRoot 'backend')
try { & $go run ./cmd/server serve; if ($LASTEXITCODE -ne 0) { throw 'Native API stopped with an error.' } } finally { Pop-Location }
