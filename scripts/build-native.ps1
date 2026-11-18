$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    cargo build --manifest-path native/Cargo.toml --release
    if ($LASTEXITCODE -ne 0) {
        throw "cargo build failed with exit code $LASTEXITCODE"
    }

    $runtimeDir = Join-Path $repoRoot "runtime"
    New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null

    $srcDll = Join-Path $repoRoot "native\target\release\khutaf_hook.dll"
    $dstDll = Join-Path $runtimeDir "khutaf-hook64.dll"
    $oldDll = "$dstDll.old"
    Remove-Item -Force $oldDll -ErrorAction SilentlyContinue

    try {
        Copy-Item -Force $srcDll $dstDll
    } catch {
        if (Test-Path $dstDll) {
            Move-Item -Force $dstDll $oldDll
        }
        Copy-Item -Force $srcDll $dstDll
    }
    Write-Host "Built $dstDll"
} finally {
    Pop-Location
}
