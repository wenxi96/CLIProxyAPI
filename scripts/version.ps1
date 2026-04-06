param(
    [ValidateSet("snapshot", "release")]
    [string]$Mode = "snapshot",
    [string]$Tag = ""
)

$ErrorActionPreference = "Stop"
$forkMark = if ($env:CPA_FORK_MARK) { $env:CPA_FORK_MARK } else { "wx" }

$rootDir = (git rev-parse --show-toplevel).Trim()
Push-Location $rootDir

try {
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
            Where-Object { $_ -match '^v\d+\.\d+\.\d+$' } |
            Select-Object -First 1

        if (-not $baseTag) {
            throw "failed to resolve upstream base tag from current branch"
        }

        $baseVersion = $baseTag.Substring(1)
        $version = "$baseVersion-$forkMark.master.$shortCommit"

        [PSCustomObject]@{
            MODE          = $Mode
            BASE_TAG      = $baseTag
            BASE_VERSION  = $baseVersion
            VERSION       = $version
            SNAPSHOT_TAG  = "v$version"
            SNAPSHOT_NAME = "snapshot-$version"
            COMMIT        = $shortCommit
            FULL_COMMIT   = $fullCommit
            BUILD_DATE    = $buildDate
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

        [PSCustomObject]@{
            MODE         = $Mode
            RELEASE_TAG  = $Tag
            RELEASE_NAME = $Tag
            VERSION      = $Tag.TrimStart("v")
            COMMIT       = $shortCommit
            FULL_COMMIT  = $fullCommit
            BUILD_DATE   = $buildDate
            SOURCE_REPOSITORY = $sourceRepository
        }
    }
}
finally {
    Pop-Location
}
