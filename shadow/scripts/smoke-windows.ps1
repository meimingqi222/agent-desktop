#Requires -Version 5.1
param(
    [string]$AdExe = (Join-Path $PSScriptRoot '..\bin\ad.exe'),
    [string]$AgentDesktopBin = $env:AD_AGENT_DESKTOP_BIN,
    [string]$FixtureExe = '',
    [switch]$Build,
    [switch]$SkipFixture,
    [switch]$TestClipboard,
    [switch]$CloseExisting
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)

$script:AdExe = [System.IO.Path]::GetFullPath($AdExe)
$script:GlobalArgs = @()
if ($AgentDesktopBin) {
    $script:GlobalArgs += @('--agent-desktop-bin', $AgentDesktopBin)
}

function Invoke-AdJson {
    param([string[]]$Argv)

    $stderrPath = Join-Path ([System.IO.Path]::GetTempPath()) ("ad-shadow-smoke-stderr-" + [guid]::NewGuid() + ".txt")
    try {
        $output = & $script:AdExe @Argv 2>$stderrPath
        $exitCode = $LASTEXITCODE
        $stderr = ''
        if (Test-Path -LiteralPath $stderrPath) {
            $stderr = [string](Get-Content -LiteralPath $stderrPath -Raw -ErrorAction SilentlyContinue)
        }
        $text = (@($output) -join "`n").Trim()
        if ($exitCode -ne 0) {
            throw "ad $($Argv -join ' ') exited $exitCode`n$text`n$stderr"
        }
        if (-not $text) {
            throw "ad $($Argv -join ' ') produced no output`n$stderr"
        }
        try {
            return $text | ConvertFrom-Json
        } catch {
            throw "ad $($Argv -join ' ') produced invalid JSON: $text`n$stderr"
        }
    } finally {
        Remove-Item -LiteralPath $stderrPath -Force -ErrorAction SilentlyContinue
    }
}

function Assert-True {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) {
        throw "smoke assertion failed: $Message"
    }
}

function Write-Step {
    param([string]$Message)
    Write-Host "[smoke] $Message"
}

if ($Build) {
    Write-Step 'building ad.exe and helper binaries'
    & (Join-Path $PSScriptRoot 'build.ps1')
    if ($LASTEXITCODE -ne 0) {
        throw "build.ps1 exited $LASTEXITCODE"
    }
}

if (-not (Test-Path -LiteralPath $script:AdExe)) {
    throw "ad.exe not found at $script:AdExe; run scripts\build.ps1 first or pass -Build"
}
if (-not $AgentDesktopBin) {
    throw 'pass -AgentDesktopBin or set AD_AGENT_DESKTOP_BIN so the in-session worker uses the intended agent-desktop binary'
}
if (-not (Test-Path -LiteralPath $AgentDesktopBin)) {
    throw "agent-desktop binary not found at $AgentDesktopBin"
}

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('ad-shadow-smoke-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tempRoot | Out-Null

$shouldClose = $false
$closeAttempted = $false
try {
    $initial = Invoke-AdJson -Argv (@('surface', 'status') + $script:GlobalArgs)
    $preexisting = $initial.data.state -ne 'idle' -and $initial.data.session_id -gt 0
    $shouldClose = (-not $preexisting) -or $CloseExisting
    if ($preexisting -and -not $CloseExisting) {
        Write-Step "attaching to pre-existing shadow session $($initial.data.session_id); it will be left running"
    }

    Write-Step 'opening shadow surface'
    $open = Invoke-AdJson -Argv (@('surface', 'open') + $script:GlobalArgs)
    Assert-True ($open.ok -eq $true) 'surface open returned ok=false'
    Assert-True ($open.data.state -eq 'ready') "surface open state was $($open.data.state)"
    Assert-True ($open.data.session_id -gt 0) 'surface open returned no session id'
    Assert-True (-not [string]::IsNullOrEmpty($open.data.pipe_name)) 'surface open returned no pipe name'
    $sessionId = [int]$open.data.session_id
    $ownership = Get-ItemProperty 'HKCU:\Software\AdShadow' -ErrorAction SilentlyContinue
    Assert-True ($null -ne $ownership -and [int]$ownership.ChildSessionId -eq $sessionId) 'durable child-session ownership record missing or has the wrong session id'
    Assert-True (-not [string]::IsNullOrEmpty([string]$ownership.ChildWinStation)) 'durable child-session ownership record has no WinStation name'
    Write-Step "shadow session $sessionId is ready"

    $surfaceStatus = Invoke-AdJson -Argv (@('surface', 'status') + $script:GlobalArgs)
    Assert-True ($surfaceStatus.data.state -eq 'ready') "surface status state was $($surfaceStatus.data.state)"
    Assert-True ([int]$surfaceStatus.data.session_id -eq $sessionId) 'surface status reported a different session id'

    $status = Invoke-AdJson -Argv (@('status') + $script:GlobalArgs)
    Assert-True ($status.data.surfaces.pip.state -eq 'ready') "status surfaces.pip.state was $($status.data.surfaces.pip.state)"
    Assert-True ([int]$status.data.surfaces.pip.session_id -eq $sessionId) 'status reported a different pip session id'

    $windows = Invoke-AdJson -Argv (@('list-windows') + $script:GlobalArgs)
    Assert-True ($windows.ok -eq $true) 'list-windows returned ok=false'
    Write-Step "listed $(@($windows.data).Count) window(s) inside the shadow session"

    if (-not $SkipFixture) {
        if (-not $FixtureExe) {
            $fixtureBuild = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..\tests\fixture-app-windows\build.ps1'))
            if (Test-Path -LiteralPath $fixtureBuild) {
                Write-Step 'building the fixture application'
                & $fixtureBuild -OutputDir $tempRoot
                if ($LASTEXITCODE -ne 0) {
                    throw "fixture build.ps1 exited $LASTEXITCODE"
                }
                $FixtureExe = Join-Path $tempRoot 'AgentDeskFixture.exe'
            }
        }
        if ($FixtureExe -and (Test-Path -LiteralPath $FixtureExe)) {
            $fixtureName = [System.IO.Path]::GetFileName($FixtureExe)
            Write-Step "launching $fixtureName inside the shadow session"
            $launch = Invoke-AdJson -Argv (@('launch', $FixtureExe, '--activate') + $script:GlobalArgs)
            Assert-True ($launch.ok -eq $true) 'fixture launch returned ok=false'

            $snapshot = Invoke-AdJson -Argv (@('snapshot', '--app', $fixtureName, '-i') + $script:GlobalArgs)
            Assert-True ($snapshot.ok -eq $true) 'fixture snapshot returned ok=false'
            Assert-True ([int]$snapshot.data.ref_count -gt 0) 'fixture snapshot returned no refs'

            $shotPath = Join-Path $tempRoot 'shadow-fixture.png'
            $shot = Invoke-AdJson -Argv (@('screenshot', $shotPath, '--app', $fixtureName) + $script:GlobalArgs)
            Assert-True ($shot.ok -eq $true) 'fixture screenshot returned ok=false'
            Assert-True ((Test-Path -LiteralPath $shotPath) -and ((Get-Item -LiteralPath $shotPath).Length -gt 0)) 'fixture screenshot file was not written'

            $closeApp = Invoke-AdJson -Argv (@('close-app', $fixtureName, '--force') + $script:GlobalArgs)
            Assert-True ($closeApp.ok -eq $true) 'fixture close-app returned ok=false'
            Write-Step 'fixture launch, snapshot, screenshot, and close passed'
        } else {
            Write-Step 'fixture executable unavailable; skipping fixture legs'
        }
    }

    if ($TestClipboard) {
        Write-Step 'testing clipboard isolation (opt-in because a failure can overwrite the parent clipboard)'
        $parentBefore = $null
        try { $parentBefore = Get-Clipboard -Raw } catch { $parentBefore = $null }
        $marker = 'ad-shadow-smoke-' + [guid]::NewGuid()
        $set = Invoke-AdJson -Argv (@('clipboard-set', $marker) + $script:GlobalArgs)
        Assert-True ($set.ok -eq $true) 'clipboard-set returned ok=false'
        $get = Invoke-AdJson -Argv (@('clipboard-get') + $script:GlobalArgs)
        Assert-True ($get.ok -eq $true) 'clipboard-get returned ok=false'
        Start-Sleep -Milliseconds 500
        $parentAfter = $null
        try { $parentAfter = Get-Clipboard -Raw } catch { $parentAfter = $null }
        if ($parentAfter -eq $marker) {
            if ($null -ne $parentBefore) {
                try { Set-Clipboard -Value $parentBefore } catch {}
            }
            throw 'child-session clipboard escaped to the parent session'
        }
        Write-Step 'clipboard isolation passed'
    }

    Write-Step 'all live smoke legs passed'
} finally {
    if ($shouldClose) {
        Write-Step 'closing shadow surface and verifying cleanup'
        try {
            $close = Invoke-AdJson -Argv (@('surface', 'close') + $script:GlobalArgs)
            $closeAttempted = $true
            if ($close.ok -ne $true) {
                throw 'surface close returned ok=false'
            }
            $deadline = (Get-Date).AddSeconds(15)
            $finalState = 'unknown'
            while ((Get-Date) -lt $deadline) {
                Start-Sleep -Milliseconds 500
                try {
                    $final = Invoke-AdJson -Argv (@('surface', 'status') + $script:GlobalArgs)
                    $finalState = [string]$final.data.state
                    if ($finalState -eq 'idle') { break }
                } catch {
                    $finalState = 'unknown'
                }
            }
            if ($finalState -ne 'idle') {
                throw "surface status did not return to idle after close (state=$finalState)"
            }
            $runKeyItem = Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -ErrorAction SilentlyContinue
            $runKey = $null
            if ($runKeyItem -and ($runKeyItem.PSObject.Properties.Name -contains 'AdShadowRemote')) {
                $runKey = $runKeyItem.AdShadowRemote
            }
            if ($runKey) {
                throw 'AdShadowRemote Run key remained after surface close'
            }
            $ownershipAfter = Get-ItemProperty 'HKCU:\Software\AdShadow' -ErrorAction SilentlyContinue
            if ($ownershipAfter -and ($ownershipAfter.PSObject.Properties.Name -contains 'ChildSessionId')) {
                throw 'AdShadow child-session ownership record remained after surface close'
            }
            Write-Step 'cleanup verification passed'
        } catch {
            if (-not $closeAttempted) {
                Write-Host "[smoke] close failed before completion: $($_.Exception.Message)"
            }
            throw
        }
    }
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host '[smoke] PASS'
