# Builds the Paperboy firmware for one hardware type with the pinned ESPHome
# version and, with -Instance, publishes the OTA image where Paperboy offers
# it (ADR 0043):
#   <instance>/firmware/<hardware>/<hardware>-<version>.bin
# The version comes from fw_version in <hardware>.yaml.
param(
    [Parameter(Mandatory = $true)][ValidateSet("sticky", "e1001", "lcd2004")][string]$Hardware,
    [string]$Instance,
    [switch]$NoCompile
)
$ErrorActionPreference = "Stop"
$esphome = "esphome==2026.10.1"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$config = Join-Path $here "$Hardware.yaml"
$yaml = [System.IO.File]::ReadAllText($config)
$version = [regex]::Match($yaml, '(?m)^\s*fw_version:\s*"?([^"\s]+)"?').Groups[1].Value
if (-not $version) { throw "fw_version not found in $config" }

$started = Get-Date
if (-not $NoCompile) {
    # ESPHome logs to stderr; Windows PowerShell 5 would treat that as an error.
    $ErrorActionPreference = "Continue"
    uvx $esphome compile $config 2>&1 | ForEach-Object { "$_" }
    $code = $LASTEXITCODE
    $ErrorActionPreference = "Stop"
    if ($code -ne 0) { throw "compile failed" }
}
$bin = Join-Path $here ".esphome/build/$Hardware/build/firmware.ota.bin"
if (-not (Test-Path $bin)) { throw "no OTA image at $bin" }
if (-not $NoCompile -and (Get-Item $bin).LastWriteTime -lt $started) {
    throw "OTA image at $bin is older than this build; check build_path"
}
if (-not $Instance) {
    Write-Host "built $Hardware ${version}: $bin"
    return
}
$dir = Join-Path $Instance "firmware/$Hardware"
New-Item -ItemType Directory -Force $dir | Out-Null
$target = Join-Path $dir "$Hardware-$version.bin"
Copy-Item $bin $target -Force
Write-Host "published $Hardware $version to $target"
