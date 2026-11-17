# Build ad.exe as a single distributable binary: it embeds
# ad-remote.exe and ad-rdphost.exe and extracts them next to itself on
# first run. Output also includes the helpers standalone for dev.
param(
    [string]$OutDir = "$PSScriptRoot\..\bin"
)

$ErrorActionPreference = 'Stop'
$root = Resolve-Path "$PSScriptRoot\.."
$emb = "$root\cmd\ad\embedded"
$out = Resolve-Path $OutDir -ErrorAction SilentlyContinue
if (-not $out) { New-Item -ItemType Directory -Force $OutDir | Out-Null; $out = Resolve-Path $OutDir }
New-Item -ItemType Directory -Force $emb | Out-Null

Push-Location $root
try {
    go build -o "$emb\ad-remote.exe" ./cmd/ad-remote
    if ($LASTEXITCODE -ne 0) { throw "go build ad-remote failed ($LASTEXITCODE)" }
    & "$env:WINDIR\Microsoft.NET\Framework64\v4.0.30319\csc.exe" /nologo /target:winexe "/out:$emb\ad-rdphost.exe" "$root\cmd\ad-rdphost\ad-rdphost.cs" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "csc ad-rdphost failed ($LASTEXITCODE)" }
    go build -tags bundle -o "$out\ad.exe" ./cmd/ad
    if ($LASTEXITCODE -ne 0) { throw "go build ad failed ($LASTEXITCODE)" }
    Copy-Item "$emb\ad-remote.exe", "$emb\ad-rdphost.exe" $out -Force
    Write-Output "built $out\ad.exe (self-contained) + ad-remote.exe + ad-rdphost.exe"
} finally {
    Pop-Location
}
