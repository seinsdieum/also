# Exercise the committed-lock and signed-policy workflow on Windows.
$ErrorActionPreference = "Stop"

$Root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$Work = Join-Path ([IO.Path]::GetTempPath()) ("also-cp-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $Work | Out-Null
$Also = Join-Path $Work "also.exe"

function Invoke-Also {
    & $script:Also @args
    if ($LASTEXITCODE -ne 0) { throw "also failed: $($args -join ' ')" }
}

function Invoke-Git {
    git @args
    if ($LASTEXITCODE -ne 0) { throw "git failed: $($args -join ' ')" }
}

try {
    & go build -trimpath -o $Also $Root
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }

    $env:HOME = Join-Path $Work "home"
    $env:USERPROFILE = $env:HOME
    $env:ALSO_HOME = Join-Path $Work "also"
    $env:ALSO_NO_UPDATE_CHECK = "1"
    New-Item -ItemType Directory -Path $env:HOME | Out-Null

    $Remote = Join-Path $Work "remote.git"
    Invoke-Git init --bare -q $Remote
    Invoke-Also setup --yes --harness claude --namespace "personal=$Remote" --default-namespace personal

    $Key = Join-Path $Work "policy.key"
    $Pub = (& $Also policy keygen --private $Key 2>$null).Trim()
    if (-not $Pub) { throw "keygen returned no public key" }
    $env:ALSO_TRUSTED_POLICY_KEYS = $Pub

    $Src = Join-Path $Work "src"
    New-Item -ItemType Directory -Path $Src | Out-Null
    Set-Location $Src
    Set-Content -Path CLAUDE.md -Value "rules"
    Invoke-Also init demo for claude
    Invoke-Also push --semver 1.0.0

    $App = Join-Path $Work "app"
    New-Item -ItemType Directory -Path $App | Out-Null
    Set-Location $App
    $Policy = @"
schema: also.policy/v1
namespaces:
  allow: [personal]
governance:
  require_signature: true
  required_approvals: 1
  required_roles: [security]
  approvers:
    - name: ci
      role: security
      key: $Pub
"@
    Set-Content -Path also.policy.yml -Value $Policy
    Invoke-Also policy sign --key $Key
    Invoke-Also policy verify --ci
    Invoke-Also install "demo@^1.0" for claude

    Invoke-Git init -q .
    Invoke-Git add also.lock also.policy.yml also.policy.sig CLAUDE.md
    Invoke-Git "-c" "user.name=ci" "-c" "user.email=ci@example.invalid" commit -qm fixture
    Invoke-Also audit --ci
    Invoke-Also audit --sbom (Join-Path $Work "sbom.json")
    if (-not (Select-String -Path (Join-Path $Work "sbom.json") -Pattern "CycloneDX" -Quiet)) {
        throw "SBOM was not written"
    }

    $App2 = Join-Path $Work "app2"
    New-Item -ItemType Directory -Path $App2 | Out-Null
    Copy-Item also.lock, also.policy.yml, also.policy.sig -Destination $App2
    Set-Location $App2
    Invoke-Git init -q .
    Invoke-Git add also.lock also.policy.yml also.policy.sig
    Invoke-Git "-c" "user.name=ci" "-c" "user.email=ci@example.invalid" commit -qm fixture
    Invoke-Also install --frozen
    Invoke-Also audit --ci

    Write-Output "WINDOWS CONTROL PLANE OK"
}
finally {
    Set-Location $Root
    Remove-Item -Recurse -Force $Work -ErrorAction SilentlyContinue
}
