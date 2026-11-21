# Builds budget.exe.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
go build -o budget.exe .
if ($LASTEXITCODE) { exit $LASTEXITCODE }
Write-Host 'Built budget.exe'
