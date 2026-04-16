param(
  [string]$InstallDir = "$HOME\.local\bin",
  [string]$Version = "latest",
  [string]$BaseUrl = "https://github.com/darthsoluke/CodexHistorySync/releases"
)

$arch = $env:PROCESSOR_ARCHITECTURE
if (-not $arch -and $env:PROCESSOR_ARCHITEW6432) {
  $arch = $env:PROCESSOR_ARCHITEW6432
}

switch ($arch.ToLower()) {
  'amd64' { $archName = 'amd64' }
  'x86_64' { $archName = 'amd64' }
  'arm64' { $archName = 'arm64' }
  default {
    throw "Unsupported architecture / 不支持的架构: $arch"
  }
}

$assetName = "codex-history-sync_windows_$archName.exe"
if ($Version -eq 'latest') {
  $downloadUrl = "$BaseUrl/latest/download/$assetName"
} else {
  $downloadUrl = "$BaseUrl/download/$Version/$assetName"
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$targetPath = Join-Path $InstallDir 'codex-history-sync.exe'

Invoke-WebRequest -Uri $downloadUrl -OutFile $targetPath
Write-Host "Installed / 已安装: $targetPath"
Write-Host ""
Write-Host "Try / 试试:"
Write-Host "  codex-history-sync --help"
