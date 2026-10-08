& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'WellWells/agentswap'
    $dir = if ($env:AGENTSWAP_INSTALL_DIR) { $env:AGENTSWAP_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'agentswap\bin' }
    $version = if ($env:AGENTSWAP_VERSION) { $env:AGENTSWAP_VERSION } else { 'latest' }
    $arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq 'Arm64') { 'arm64' } else { 'amd64' }
    $base = if ($env:AGENTSWAP_DOWNLOAD_URL) { $env:AGENTSWAP_DOWNLOAD_URL.TrimEnd('/') } elseif ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
    $asset = "agentswap_windows_$arch.zip"

    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading $asset ($version)"
        Invoke-WebRequest "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
        Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing
        $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($asset))$" }
        $expected = if ($line) { ($line -split ' ')[0] } else { '' }
        $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
        if (-not $expected -or $expected -ne $actual) { throw "checksum mismatch for $asset" }

        Expand-Archive (Join-Path $tmp $asset) -DestinationPath $tmp -Force
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        $exe = Join-Path $dir 'agentswap.exe'
        Copy-Item (Join-Path $tmp 'agentswap.exe') $exe -Force
        Unblock-File $exe
        & $exe link
        if ($LASTEXITCODE -ne 0) { throw 'agentswap link failed' }
    } finally {
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    $userPath = [string][Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (($userPath -split ';') -contains $dir)) {
        [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir").TrimStart(';'), 'User')
        Write-Host "Added $dir to your user PATH (open a new terminal)."
    }
    if (-not (($env:Path -split ';') -contains $dir)) { $env:Path = "$env:Path;$dir" }
    Write-Host "Installed agentswap to $dir"
}
