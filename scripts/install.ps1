# Install in the current PowerShell session with:
#   irm https://raw.githubusercontent.com/michaelmjhhhh/mark-Xmind-down/main/scripts/install.ps1 | iex
# No administrator access or Go installation is required.
& {
    function Test-XmindWindows {
        return [Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT
    }

    function Get-XmindNativeArchitecture {
        # This variable identifies the native system when PowerShell is emulated.
        if ($env:PROCESSOR_ARCHITEW6432) { return $env:PROCESSOR_ARCHITEW6432 }
        try {
            return [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        } catch {
            # Windows PowerShell can run on older .NET Framework installations.
            return $env:PROCESSOR_ARCHITECTURE
        }
    }

    function Resolve-XmindArchitecture {
        param([string] $Architecture)
        switch ($Architecture.ToLowerInvariant()) {
            'amd64' { return 'amd64' }
            'x64' { return 'amd64' }
            'arm64' { return 'arm64' }
            default { throw "Unsupported Windows architecture: $Architecture. An x64 or ARM64 system is required." }
        }
    }

    function Get-XmindLocalApplicationData {
        return [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    }

    function Get-XmindUserPath {
        return [Environment]::GetEnvironmentVariable('Path', 'User')
    }

    function Set-XmindUserPath {
        param([string] $Value)
        [Environment]::SetEnvironmentVariable('Path', $Value, 'User')
    }

    function Add-XmindPath {
        param([AllowNull()][string] $Existing, [string] $Directory)
        if ([string]::IsNullOrEmpty($Existing)) { return $Directory }
        $remaining = @()
        foreach ($entry in ($Existing -split ';')) {
            $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim().Trim('"')).TrimEnd('\', '/')
            if (-not [string]::Equals($expanded, $Directory.TrimEnd('\', '/'), [StringComparison]::OrdinalIgnoreCase)) {
                $remaining += $entry
            }
        }
        # Keep the installed version ahead of older copies (for example in Go/bin).
        # Preserve the spelling, order, and empty entries of all unrelated paths.
        return (@($Directory) + $remaining) -join ';'
    }

    function Invoke-XmindDownload {
        param([string] $Uri, [string] $Destination)
        $previousProtocol = [Net.ServicePointManager]::SecurityProtocol
        $ProgressPreference = 'SilentlyContinue'
        try {
            # GitHub requires TLS 1.2, including on Windows PowerShell 5.1.
            [Net.ServicePointManager]::SecurityProtocol = $previousProtocol -bor [Net.SecurityProtocolType]::Tls12
            Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination -ErrorAction Stop
        } finally {
            [Net.ServicePointManager]::SecurityProtocol = $previousProtocol
        }
    }

    function Install-XmindMD {
        $ErrorActionPreference = 'Stop'
        if (-not (Test-XmindWindows)) { throw 'This installer requires Windows. Use scripts/install.sh on macOS or Linux.' }
        $architecture = Resolve-XmindArchitecture (Get-XmindNativeArchitecture)
        $asset = "xmind-md-windows-$architecture.exe"
        $base = 'https://github.com/michaelmjhhhh/mark-Xmind-down/releases/latest/download'
        if ($env:XMIND_MD_VERSION) {
            if ($env:XMIND_MD_VERSION -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$') {
                throw 'XMIND_MD_VERSION must be a version tag such as v1.0.0.'
            }
            $base = "https://github.com/michaelmjhhhh/mark-Xmind-down/releases/download/$env:XMIND_MD_VERSION"
        }
        $localData = Get-XmindLocalApplicationData
        if ([string]::IsNullOrWhiteSpace($localData)) { throw 'The Windows local application data directory is unavailable.' }
        $installDirectory = Join-Path $localData 'Programs\xmind-md'
        if ($installDirectory.Contains(';')) { throw 'The installation directory contains a semicolon and cannot be added to PATH.' }
        $destination = Join-Path $installDirectory 'xmind-md.exe'
        $temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ('xmind-md-' + [Guid]::NewGuid().ToString('N'))
        $staged = $null
        try {
            [void][IO.Directory]::CreateDirectory($temporaryDirectory)
            $downloaded = Join-Path $temporaryDirectory $asset
            $checksums = Join-Path $temporaryDirectory 'SHA256SUMS'
            Write-Host "Downloading xmind-md for Windows $architecture..."
            Invoke-XmindDownload "$base/$asset" $downloaded
            Invoke-XmindDownload "$base/SHA256SUMS" $checksums
            if (-not [IO.File]::Exists($downloaded) -or (Get-Item -LiteralPath $downloaded).Length -eq 0) {
                throw 'The downloaded executable is missing or empty. The existing installation has not been changed.'
            }
            $expected = @(
                foreach ($line in [IO.File]::ReadAllLines($checksums)) {
                    if ($line -match '^([a-fA-F0-9]{64})[ \t]+\*?([^ \t]+)[ \t]*$' -and $Matches[2] -ceq $asset) {
                        $Matches[1]
                    }
                }
            )
            if ($expected.Count -ne 1) { throw "SHA256SUMS must contain exactly one checksum for $asset." }
            $actual = (Get-FileHash -LiteralPath $downloaded -Algorithm SHA256).Hash
            if ($actual -ine $expected[0]) { throw 'Checksum verification failed. The existing installation has not been changed.' }

            # Stage beside the destination, then replace atomically; an in-use binary
            # or a failed download must never leave a truncated installation behind.
            [void][IO.Directory]::CreateDirectory($installDirectory)
            $staged = Join-Path $installDirectory ('.xmind-md-' + [Guid]::NewGuid().ToString('N') + '.tmp')
            [IO.File]::Copy($downloaded, $staged)
            if ([IO.File]::Exists($destination)) {
                [IO.File]::Replace($staged, $destination, [Management.Automation.Language.NullString]::Value)
            } else {
                [IO.File]::Move($staged, $destination)
            }
            $existingUserPath = Get-XmindUserPath
            $updatedUserPath = Add-XmindPath $existingUserPath $installDirectory
            if ($updatedUserPath -cne $existingUserPath) { Set-XmindUserPath $updatedUserPath }
            $env:Path = Add-XmindPath $env:Path $installDirectory
            Write-Host "Installed: $destination"
            Write-Host 'Ready. Run: xmind-md'
        } finally {
            if ($staged -and [IO.File]::Exists($staged)) { [IO.File]::Delete($staged) }
            if ([IO.Directory]::Exists($temporaryDirectory)) { [IO.Directory]::Delete($temporaryDirectory, $true) }
        }
    }

    Install-XmindMD
}
