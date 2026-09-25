# Isolated installer checks; no network, administrator access, or real User PATH edits.
# Run on Windows: powershell -NoProfile -File scripts/test-install.ps1
# Also supported: pwsh -NoProfile -File scripts/test-install.ps1
$ErrorActionPreference = 'Stop'
$installer = Join-Path $PSScriptRoot 'install.ps1'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($installer, [ref] $tokens, [ref] $parseErrors)
if ($parseErrors.Count -ne 0) { throw ($parseErrors | Out-String) }
# Load only functions, so the production entry point cannot change this machine.
foreach ($definition in $ast.FindAll({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] }, $true)) {
    . ([scriptblock]::Create($definition.Extent.Text))
}

function Assert-Equal {
    param($Actual, $Expected, [string] $Message)
    if ($Actual -cne $Expected) { throw "$Message`nExpected: $Expected`nActual: $Actual" }
}
function Assert-True {
    param([bool] $Condition, [string] $Message)
    if (-not $Condition) { throw $Message }
}
function Assert-Fails {
    param([scriptblock] $Action, [string] $Pattern)
    $failure = $null
    try { & $Action } catch { $failure = $_.Exception.Message }
    if (-not $failure -or $failure -notlike $Pattern) { throw "Expected failure '$Pattern', received '$failure'." }
}

$testRoot = Join-Path ([IO.Path]::GetTempPath()) ('xmind installer tests ' + [Guid]::NewGuid().ToString('N'))
$savedProcessPath = $env:Path
$savedVersion = $env:XMIND_MD_VERSION
$savedNativeArchitecture = $env:PROCESSOR_ARCHITEW6432
$savedTestInstallDirectory = $env:XMIND_TEST_INSTALL_DIRECTORY
$script:testArchitecture = 'AMD64'
$script:testUserPath = 'C:\Existing Tools;C:\Other Tools'
$script:pathWrites = 0
$script:downloadCalls = @()
$script:downloadDirectories = @()
$script:downloadMode = 'valid'
$script:binaryBytes = [Text.Encoding]::UTF8.GetBytes('verified binary fixture')
$hasher = [Security.Cryptography.SHA256]::Create()
try { $script:binaryHash = ([BitConverter]::ToString($hasher.ComputeHash($script:binaryBytes))).Replace('-', '').ToLowerInvariant() }
finally { $hasher.Dispose() }

function Test-XmindWindows { return $true }
function Get-XmindLocalApplicationData { return $testRoot }
function Get-XmindUserPath { return $script:testUserPath }
function Set-XmindUserPath {
    param([string] $Value)
    $script:testUserPath = $Value
    $script:pathWrites++
}
function Invoke-XmindDownload {
    param([string] $Uri, [string] $Destination)
    $script:downloadCalls += $Uri
    $script:downloadDirectories += [IO.Path]::GetDirectoryName($Destination)
    if ($script:downloadMode -eq 'fail') { throw 'Simulated download failure.' }
    if ($Uri.EndsWith('/SHA256SUMS')) {
        $name = "xmind-md-windows-$script:testArchitecture.exe"
        $hash = $script:binaryHash
        if ($script:downloadMode -eq 'empty-download') { $hash = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' }
        if ($script:downloadMode -eq 'bad-checksum') { $hash = '0' * 64 }
        if ($script:downloadMode -eq 'missing-checksum') { $name = 'other-file.exe' }
        $line = "$hash  $name`n"
        if ($script:downloadMode -eq 'duplicate-checksum') { $line += $line }
        [IO.File]::WriteAllText($Destination, $line)
    } else {
        if ($script:downloadMode -eq 'missing-download') { return }
        $bytes = $script:binaryBytes
        if ($script:downloadMode -eq 'empty-download') { $bytes = [byte[]]@() }
        [IO.File]::WriteAllBytes($Destination, $bytes)
    }
}

try {
    [void][IO.Directory]::CreateDirectory($testRoot)
    Assert-Equal (Resolve-XmindArchitecture 'AMD64') 'amd64' 'AMD64 mapping'
    Assert-Equal (Resolve-XmindArchitecture 'X64') 'amd64' 'Runtime X64 mapping'
    Assert-Equal (Resolve-XmindArchitecture 'ARM64') 'arm64' 'ARM64 mapping'
    Assert-Fails { Resolve-XmindArchitecture 'x86' } '*Unsupported*'
    $env:PROCESSOR_ARCHITEW6432 = 'ARM64'
    Assert-Equal (Get-XmindNativeArchitecture) 'ARM64' 'Emulated shells must use native architecture'
    function Get-XmindNativeArchitecture { return $script:testArchitecture }
    $script:testArchitecture = 'amd64'
    $env:XMIND_MD_VERSION = $null
    $env:Path = 'C:\Process Tools;C:\Windows\System32'
    $installDirectory = Join-Path $testRoot 'Programs\xmind-md'
    $destination = Join-Path $installDirectory 'xmind-md.exe'
    $script:downloadMode = 'bad-checksum'
    Assert-Fails { Install-XmindMD } '*Checksum verification failed*'
    Assert-True (-not [IO.Directory]::Exists($installDirectory)) 'A failed first install creates no installation directory'
    Assert-Equal $script:pathWrites 0 'A failed first install never writes persistent PATH'
    Assert-Equal $env:Path 'C:\Process Tools;C:\Windows\System32' 'A failed first install preserves process PATH'
    $script:downloadMode = 'valid'
    Install-XmindMD
    Assert-Equal ([IO.File]::ReadAllText($destination)) 'verified binary fixture' 'Installed bytes'
    Assert-Equal $script:testUserPath "$installDirectory;C:\Existing Tools;C:\Other Tools" 'Persistent User PATH preserves unrelated entries'
    Assert-Equal $env:Path "$installDirectory;C:\Process Tools;C:\Windows\System32" 'Current process PATH is updated immediately'
    Assert-Equal $script:pathWrites 1 'First install writes User PATH once'
    Assert-True ($script:downloadCalls[0] -eq 'https://github.com/michaelmjhhhh/mark-Xmind-down/releases/latest/download/xmind-md-windows-amd64.exe') 'Default latest release URL'

    Install-XmindMD
    Assert-Equal $script:pathWrites 1 'Reinstall must not rewrite or duplicate PATH'
    Assert-Equal $script:testUserPath "$installDirectory;C:\Existing Tools;C:\Other Tools" 'Reinstall is idempotent'
    Assert-Equal (Add-XmindPath ('"' + $installDirectory.ToUpperInvariant() + '\";C:\Other') $installDirectory) "$installDirectory;C:\Other" 'Quoted paths, case, and trailing slash are equivalent'
    Assert-Equal (Add-XmindPath $null $installDirectory) $installDirectory 'Empty PATH has no stray separator'

    $env:XMIND_TEST_INSTALL_DIRECTORY = $installDirectory
    $oldPath = 'C:\Old Go\bin;;"' + $installDirectory.ToUpperInvariant() + '\";C:\Other;%XMIND_TEST_INSTALL_DIRECTORY%\;' + $installDirectory + ';'
    $script:testUserPath = $oldPath
    $env:Path = $oldPath
    Install-XmindMD
    $preferredPath = "$installDirectory;C:\Old Go\bin;;C:\Other;"
    Assert-Equal $script:testUserPath $preferredPath 'Install moves its directory ahead of old Go/bin and removes equivalent duplicates'
    Assert-Equal $env:Path $preferredPath 'Current PATH also prefers the installed version and preserves unrelated empty entries'
    Assert-Equal $script:pathWrites 2 'Changing precedence writes User PATH once'
    Install-XmindMD
    Assert-Equal $script:testUserPath $preferredPath 'Repeated install keeps identical PATH'
    Assert-Equal $env:Path $preferredPath 'Repeated install keeps identical process PATH'
    Assert-Equal $script:pathWrites 2 'Repeated install does not write User PATH again'

    $script:testArchitecture = 'arm64'
    $env:XMIND_MD_VERSION = 'v1.2.3'
    $script:downloadCalls = @()
    Install-XmindMD
    Assert-Equal $script:downloadCalls[0] 'https://github.com/michaelmjhhhh/mark-Xmind-down/releases/download/v1.2.3/xmind-md-windows-arm64.exe' 'Pinned ARM64 release'

    $beforeUserPath = $script:testUserPath
    $beforeProcessPath = $env:Path
    $beforePathWrites = $script:pathWrites
    foreach ($mode in @('bad-checksum', 'missing-checksum', 'duplicate-checksum', 'empty-download', 'missing-download', 'fail')) {
        $script:downloadMode = $mode
        [IO.File]::WriteAllText($destination, 'previous installation')
        Assert-Fails { Install-XmindMD } '*'
        Assert-Equal ([IO.File]::ReadAllText($destination)) 'previous installation' "$mode preserves installed binary"
        Assert-Equal $script:testUserPath $beforeUserPath "$mode preserves persistent PATH"
        Assert-Equal $env:Path $beforeProcessPath "$mode preserves current PATH"
        Assert-Equal $script:pathWrites $beforePathWrites "$mode must not write User PATH"
        Assert-Equal @(Get-ChildItem -LiteralPath $installDirectory -Filter '*.tmp' -Force).Count 0 "$mode cleans staged files"
    }
    $script:downloadMode = 'valid'
    $script:downloadCalls = @()
    $env:XMIND_MD_VERSION = '../../bad'
    Assert-Fails { Install-XmindMD } '*XMIND_MD_VERSION*'
    Assert-Equal $script:downloadCalls.Count 0 'Invalid version is rejected before download'
    $env:XMIND_MD_VERSION = $null
    $script:testArchitecture = 'x86'
    Assert-Fails { Install-XmindMD } '*Unsupported*'
    Assert-Equal $script:downloadCalls.Count 0 'Unsupported architecture is rejected before download'
    foreach ($directory in $script:downloadDirectories) {
        Assert-True (-not [IO.Directory]::Exists($directory)) 'Download staging directories are removed after success and failure'
    }
    Write-Host 'All Windows installer checks passed.'
} finally {
    $env:Path = $savedProcessPath
    $env:XMIND_MD_VERSION = $savedVersion
    $env:PROCESSOR_ARCHITEW6432 = $savedNativeArchitecture
    $env:XMIND_TEST_INSTALL_DIRECTORY = $savedTestInstallDirectory
    if ([IO.Directory]::Exists($testRoot)) { [IO.Directory]::Delete($testRoot, $true) }
}
