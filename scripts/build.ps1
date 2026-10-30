<#
.SYNOPSIS
Builds the programs for multiple target systems into bin/<os>-<arch>/.

.EXAMPLE
./scripts/build.ps1                                  # all programs, all targets
./scripts/build.ps1 -Targets linux/amd64,linux/arm64  # Linux only
./scripts/build.ps1 -Apps paperboy -Targets linux/arm64
#>
param(
    [string[]]$Targets = @('linux/amd64', 'linux/arm64', 'windows/amd64'),
    [string[]]$Apps = @('paperboy', 'odtest', 'weather')
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
$failed = @()

Push-Location $root
try {
    # Release data for the dashboard header. Go stamps VCS information by
    # itself, but only when it finds the repository and buildvcs is enabled;
    # explicit -X values keep the header useful in every case.
    $version = ''
    $commit = ''
    $buildDate = ''
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $commit = (git rev-parse HEAD 2>$null)
        if ($LASTEXITCODE -eq 0) {
            $buildDate = (git log -1 --format=%cI 2>$null)
            if ((git status --porcelain 2>$null | Measure-Object).Count -gt 0) { $commit = "$commit-dirty" }
            if ((git tag 2>$null | Measure-Object).Count -gt 0) {
                $tag = (git describe --tags --abbrev=0 2>$null)
                if ($LASTEXITCODE -eq 0 -and $tag) { $version = $tag }
            }
        } else {
            $commit = ''
        }
    }
    $ldflags = '-s -w'
    if ($version) { $ldflags += " -X github.com/maehld/paperboy/internal/paperboy.buildVersionTag=$version" }
    if ($commit) { $ldflags += " -X github.com/maehld/paperboy/internal/paperboy.buildCommit=$commit" }
    if ($buildDate) { $ldflags += " -X github.com/maehld/paperboy/internal/paperboy.buildTime=$buildDate" }

    foreach ($target in $Targets) {
        $goos, $goarch = $target.Split('/')
        $env:GOOS, $env:GOARCH, $env:CGO_ENABLED = $goos, $goarch, '0'
        $ext = if ($goos -eq 'windows') { '.exe' } else { '' }
        foreach ($app in $Apps) {
            $out = Join-Path 'bin' "$goos-$goarch/$app$ext"
            Write-Host "Building $out"
            go build -trimpath -ldflags $ldflags -o $out "./cmd/$app"
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
    Write-Error "Failed: $($failed -join ', ')"
    exit 1
}
Write-Host 'Done.'
