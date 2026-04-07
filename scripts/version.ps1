param(
    [ValidateSet("snapshot", "release")]
    [string]$Mode = "snapshot",
    [string]$Tag = ""
)

$ErrorActionPreference = "Stop"

$rootDir = (git rev-parse --show-toplevel).Trim()
Push-Location $rootDir

try {
    $customMark = "wx"
    $customVersion = "1"
    $upstreamTagRegex = '^v\d+\.\d+\.\d+$'
    $metadataPath = Join-Path $rootDir "release-metadata.env"
    if (Test-Path $metadataPath) {
        foreach ($line in Get-Content $metadataPath) {
            if ([string]::IsNullOrWhiteSpace($line) -or $line.StartsWith("#") -or -not $line.Contains("=")) {
                continue
            }
            $parts = $line -split "=", 2
            switch ($parts[0]) {
                "CUSTOM_MARK" { $customMark = $parts[1] }
                "CUSTOM_VERSION" { $customVersion = $parts[1] }
                "UPSTREAM_TAG_REGEX" { $upstreamTagRegex = $parts[1] }
            }
        }
    }

    $shortCommit = (git rev-parse --short HEAD).Trim()
    $fullCommit = (git rev-parse HEAD).Trim()
    $buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    $remoteUrl = ""
    try {
        $remoteUrl = (git remote get-url origin).Trim()
    }
    catch {
        $remoteUrl = ""
    }

    $sourceRepository = ""
    if ($remoteUrl -match '^https?://github\.com/([^/]+)/([^/]+?)(\.git)?$') {
        $repoName = ($Matches[2] -replace '\.git$', '')
        $sourceRepository = "https://github.com/$($Matches[1])/$repoName"
    }
    elseif ($remoteUrl -match '^git@github\.com:([^/]+)/([^/]+?)(\.git)?$') {
        $repoName = ($Matches[2] -replace '\.git$', '')
        $sourceRepository = "https://github.com/$($Matches[1])/$repoName"
    }
    elseif ($remoteUrl -match '^[^/]+:([^/]+)/([^/]+?)(\.git)?$') {
        $repoName = ($Matches[2] -replace '\.git$', '')
        $sourceRepository = "https://github.com/$($Matches[1])/$repoName"
    }
    elseif ($remoteUrl) {
        $sourceRepository = $remoteUrl -replace '\.git$', ''
    }

    if ($Mode -eq "snapshot") {
        $baseTag = git tag --merged HEAD --list "v*" --sort=-version:refname |
            Where-Object { $_ -match $upstreamTagRegex } |
            Select-Object -First 1

        if (-not $baseTag) {
            throw "failed to resolve upstream base tag from current branch"
        }

        $baseVersion = $baseTag.Substring(1)
        $version = "$baseVersion-$customMark.$customVersion"
        $snapshotTag = "v$version-build.$shortCommit"

        [PSCustomObject]@{
            MODE              = $Mode
            BASE_TAG          = $baseTag
            BASE_VERSION      = $baseVersion
            CUSTOM_MARK       = $customMark
            CUSTOM_VERSION    = $customVersion
            VERSION           = $version
            SNAPSHOT_TAG      = $snapshotTag
            SNAPSHOT_NAME     = $version
            COMMIT            = $shortCommit
            FULL_COMMIT       = $fullCommit
            BUILD_DATE        = $buildDate
            SOURCE_REPOSITORY = $sourceRepository
        }
    }
    else {
        if (-not $Tag) {
            $Tag = $env:GITHUB_REF_NAME
        }
        if (-not $Tag) {
            $Tag = (git describe --tags --exact-match 2>$null).Trim()
        }
        if (-not $Tag) {
            throw "failed to resolve release tag"
        }
        $version = $Tag.TrimStart("v") -replace '-build\.[0-9a-f]+$',''

        [PSCustomObject]@{
            MODE              = $Mode
            RELEASE_TAG       = $Tag
            RELEASE_NAME      = $version
            VERSION           = $version
            COMMIT            = $shortCommit
            FULL_COMMIT       = $fullCommit
            BUILD_DATE        = $buildDate
            SOURCE_REPOSITORY = $sourceRepository
        }
    }
}
finally {
    Pop-Location
}
