param(
    [string[]]$Files = @("build/bin/Khutaf.exe", "runtime/khutaf-hook64.dll"),
    [string]$PfxPath = $env:KHUTAF_SIGN_PFX,
    [string]$PfxPassword = $env:KHUTAF_SIGN_PASSWORD,
    [string]$Thumbprint = $env:KHUTAF_SIGN_THUMBPRINT,
    [string]$TimestampUrl = "http://timestamp.digicert.com"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($PfxPath) -and [string]::IsNullOrWhiteSpace($Thumbprint)) {
    Write-Error "Provide a code-signing certificate via -PfxPath (or KHUTAF_SIGN_PFX) or -Thumbprint (or KHUTAF_SIGN_THUMBPRINT)."
    exit 1
}

if (-not [string]::IsNullOrWhiteSpace($PfxPath) -and -not (Test-Path -LiteralPath $PfxPath)) {
    Write-Error "PFX certificate not found: $PfxPath"
    exit 1
}

function Find-SignTool {
    $cmd = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($cmd) {
        return $cmd.Source
    }
    $kitsRoot = Join-Path ${env:ProgramFiles(x86)} "Windows Kits\10\bin"
    if (Test-Path -LiteralPath $kitsRoot) {
        $found = Get-ChildItem -Path $kitsRoot -Filter "signtool.exe" -Recurse -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\signtool\.exe$' } |
            Sort-Object FullName -Descending |
            Select-Object -First 1
        if ($found) {
            return $found.FullName
        }
    }
    return $null
}

$signTool = Find-SignTool
if (-not $signTool) {
    Write-Error "signtool.exe not found in PATH or Windows Kits 10 SDK."
    exit 1
}

foreach ($f in $Files) {
    if (-not (Test-Path -LiteralPath $f)) {
        Write-Error "Target binary not found: $f"
        exit 1
    }
}

foreach ($f in $Files) {
    $signArgs = @("sign", "/fd", "SHA256", "/tr", $TimestampUrl, "/td", "SHA256")
    if (-not [string]::IsNullOrWhiteSpace($PfxPath)) {
        $signArgs += @("/f", $PfxPath)
        if (-not [string]::IsNullOrWhiteSpace($PfxPassword)) {
            $signArgs += @("/p", $PfxPassword)
        }
    } else {
        $signArgs += @("/sha1", $Thumbprint)
    }
    $signArgs += $f

    Write-Host "Signing $f..."
    & $signTool @signArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Error "signtool sign failed for $f (exit $LASTEXITCODE)"
        exit $LASTEXITCODE
    }

    & $signTool verify /pa $f
    if ($LASTEXITCODE -ne 0) {
        Write-Error "signtool verify failed for $f (exit $LASTEXITCODE)"
        exit $LASTEXITCODE
    }
}

Write-Host "Successfully signed and verified $($Files.Count) file(s)."
