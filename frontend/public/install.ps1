$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$InstallRoot = if ($env:TERASLACK_INSTALL_ROOT) { $env:TERASLACK_INSTALL_ROOT } else { Join-Path $HOME ".teraslack" }
$BinDir = Join-Path $InstallRoot "bin"
$ConfigFile = Join-Path $InstallRoot "config.json"
$ApiBaseUrl = if ($env:TERASLACK_INSTALL_API_URL) { $env:TERASLACK_INSTALL_API_URL } elseif ($env:TERASLACK_API_BASE_URL) { $env:TERASLACK_API_BASE_URL } else { "https://api.teraslack.ai" }
$DownloadBaseUrl = if ($env:TERASLACK_DOWNLOAD_BASE_URL) { $env:TERASLACK_DOWNLOAD_BASE_URL } else { "https://downloads.teraslack.ai/teraslack/cli" }
$ManifestUrl = if ($env:TERASLACK_CLI_MANIFEST_URL) { $env:TERASLACK_CLI_MANIFEST_URL } else { "$DownloadBaseUrl/latest.json" }

function Write-Log {
  param([string]$Message)
  Write-Host $Message
}

function Fail {
  param([string]$Message)
  throw $Message
}

function Get-Platform {
  $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
  switch ($arch) {
    "x64" { return "windows-amd64" }
    "arm64" { return "windows-arm64" }
    default { Fail "Unsupported Windows architecture: $arch" }
  }
}

function New-TempDir {
  $path = Join-Path ([System.IO.Path]::GetTempPath()) ("teraslack-install-" + [System.Guid]::NewGuid().ToString("N"))
  New-Item -ItemType Directory -Force -Path $path | Out-Null
  return $path
}

function Write-Config {
  New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null

  $payload = @{}
  if (Test-Path -LiteralPath $ConfigFile) {
    try {
      $existing = Get-Content -LiteralPath $ConfigFile -Raw | ConvertFrom-Json -AsHashtable
      if ($existing) {
        $payload = $existing
      }
    } catch {
      $payload = @{}
    }
  }

  $payload["base_url"] = $ApiBaseUrl
  Set-Content -Path $ConfigFile -Value (($payload | ConvertTo-Json) + [Environment]::NewLine) -Encoding UTF8
}

function Ensure-Path {
  $currentUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
  $entries = @()
  if ($currentUserPath) {
    $entries = $currentUserPath.Split(";", [System.StringSplitOptions]::RemoveEmptyEntries)
  }

  $existing = $entries | Where-Object { $_.TrimEnd("\") -ieq $BinDir.TrimEnd("\") }
  if (-not $existing) {
    $newPath = if ($currentUserPath) { "$BinDir;$currentUserPath" } else { $BinDir }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Log "Added $BinDir to user PATH"
  }

  if (-not (($env:Path -split ";") | Where-Object { $_.TrimEnd("\") -ieq $BinDir.TrimEnd("\") })) {
    $env:Path = "$BinDir;$env:Path"
  }
}

function Verify-Sha256 {
  param(
    [string]$FilePath,
    [string]$Expected
  )

  $actual = (Get-FileHash -Algorithm SHA256 -Path $FilePath).Hash.ToLowerInvariant()
  if ($actual -ne $Expected.ToLowerInvariant()) {
    Fail "SHA256 mismatch for downloaded CLI binary"
  }
}

function Install-Binary {
  param([string]$Platform)

  $tempDir = New-TempDir
  try {
    Write-Log "Resolving Teraslack CLI binary for $Platform..."
    $manifest = Invoke-RestMethod -Method Get -Uri $ManifestUrl
    $artifact = $manifest.artifacts.$Platform
    if (-not $artifact) {
      Fail "No prebuilt CLI binary is available for platform $Platform"
    }

    $binaryName = if ($artifact.binary_name) { [string]$artifact.binary_name } else { "teraslack.exe" }
    $artifactUrl = [string]$artifact.url
    $artifactSha256 = [string]$artifact.sha256
    if (-not $artifactUrl) {
      Fail "Manifest did not include an artifact URL for $Platform"
    }
    if (-not $artifactSha256) {
      Fail "Manifest did not include a SHA256 for $Platform"
    }

    $archivePath = Join-Path $tempDir ([System.IO.Path]::GetFileName($artifactUrl))
    $extractDir = Join-Path $tempDir "extract"

    Write-Log "Downloading Teraslack CLI $($manifest.version) for $Platform..."
    Invoke-WebRequest -Uri $artifactUrl -OutFile $archivePath
    Verify-Sha256 -FilePath $archivePath -Expected $artifactSha256

    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force

    $downloadedBinary = Join-Path $extractDir $binaryName
    if (-not (Test-Path -LiteralPath $downloadedBinary)) {
      Fail "Downloaded archive did not contain $binaryName"
    }

    $installedBinaryPath = Join-Path $BinDir $binaryName
    Move-Item -Force -Path $downloadedBinary -Destination $installedBinaryPath
    return $installedBinaryPath
  } finally {
    if (Test-Path -LiteralPath $tempDir) {
      Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
  }
}

Write-Config
$installedBinaryPath = Install-Binary -Platform (Get-Platform)
Ensure-Path

Write-Log ""
Write-Log "Teraslack CLI installed."
Write-Log "Config: $ConfigFile"
Write-Log "Binary: $installedBinaryPath"
Write-Log ""
Write-Log "Open a new shell and run:"
Write-Log "  teraslack signin email --email you@example.com"
Write-Log ""
Write-Log "Then you can verify connectivity with:"
Write-Log "  teraslack health"
Write-Log "  teraslack me"
