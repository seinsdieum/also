# Install also on Windows from GitHub releases.
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/seinsdieum/also/main/install.ps1 | iex"
$ErrorActionPreference = "Stop"
$Repo = "seinsdieum/also"
$InstallDir = if ($env:ALSO_INSTALL_DIR) { $env:ALSO_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "also\bin" }
$Base = if ($env:ALSO_BASE_URL) { $env:ALSO_BASE_URL } else { "https://github.com/$Repo" }
$Version = $env:ALSO_VERSION

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  "AMD64" { "amd64" }
  "ARM64" { "arm64" }
  default { throw "also: unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}
$asset = "also-windows-$arch.exe"
$url = if ($Version) { "$Base/releases/download/v$($Version.TrimStart('v'))" } else { "$Base/releases/latest/download" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("also-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "also: downloading $asset"
  Invoke-WebRequest "$url/$asset" -OutFile (Join-Path $tmp $asset)
  Invoke-WebRequest "$url/SHA256SUMS" -OutFile (Join-Path $tmp "SHA256SUMS")
  $expected = (Get-Content (Join-Path $tmp "SHA256SUMS") | Where-Object { $_ -split '\s+' -contains $asset } | ForEach-Object { ($_ -split '\s+')[0] } | Select-Object -First 1)
  if (-not $expected) { throw "also: SHA256SUMS has no entry for $asset" }
  $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
  if ($expected.ToLower() -ne $actual) { throw "also: checksum mismatch for $asset" }
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Copy-Item (Join-Path $tmp $asset) (Join-Path $InstallDir "also.exe") -Force
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ';') -notcontains $InstallDir) {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
  Write-Host "also: added $InstallDir to your user PATH; restart the terminal"
}
Write-Host "also: installed to $InstallDir\also.exe"
Write-Host "also: next run: also setup"
