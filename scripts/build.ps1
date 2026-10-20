<#
.SYNOPSIS
Baut die Programme für mehrere Zielsysteme nach bin/<os>-<arch>/.

.EXAMPLE
./scripts/build.ps1                                  # alle Programme, alle Ziele
./scripts/build.ps1 -Targets linux/amd64,linux/arm64  # nur Linux
./scripts/build.ps1 -Apps paperboy -Targets linux/arm64
#>
param(
    [string[]]$Targets = @('linux/amd64', 'linux/arm64', 'windows/amd64'),
    [string[]]$Apps = @('paperboy', 'odtest')
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
$failed = @()

Push-Location $root
try {
    foreach ($target in $Targets) {
        $goos, $goarch = $target.Split('/')
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $goos, $goarch, '0'
        $ext = if ($goos -eq 'windows') { '.exe' } else { '' }
        foreach ($app in $Apps) {
            $out = Join-Path 'bin' "$goos-$goarch/$app$ext"
            Write-Host "Baue $out"
            go build -trimpath -ldflags '-s -w' -o $out "./cmd/$app"
            if ($LASTEXITCODE -ne 0) { $failed += "$target/$app" }
        }
    }
}
finally {
    foreach ($k in $saved.Keys) { Set-Item "env:$k" -Value $saved[$k] -ErrorAction SilentlyContinue }
    foreach ($k in $saved.Keys) { if ($null -eq $saved[$k]) { Remove-Item "env:$k" -ErrorAction SilentlyContinue } }
    Pop-Location
}

if ($failed) {
    Write-Error "Fehlgeschlagen: $($failed -join ', ')"
    exit 1
}
Write-Host 'Fertig.'
