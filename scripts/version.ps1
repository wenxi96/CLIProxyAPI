param(
    [ValidateSet("auto-release", "snapshot", "release")]
    [string]$Mode = "auto-release",
    [string]$Tag = ""
)

$ErrorActionPreference = "Stop"

function Normalize-ReleaseVersion {
    param([string]$Raw)

    $value = ($Raw ?? "").Trim()
    $value = $value.TrimStart("v")
    return ($value -replace '-build\.[0-9a-f]+$', '').Trim()
}

function Extract-ForkVersion {
    param([string]$Raw, [string]$CustomMark)

    $value = Normalize-ReleaseVersion $Raw
    if ($value -match "-$([regex]::Escape($CustomMark))-(.+)$") {
        return $Matches[1]
    }
    if ($value -match "-$([regex]::Escape($CustomMark))\.(.+)$") {
        return $Matches[1]
    }
    return $null
}

function Extract-BaseVersion {
    param([string]$Raw, [string]$CustomMark)

    $value = Normalize-ReleaseVersion $Raw
    if ($value -match "^(.*)-$([regex]::Escape($CustomMark))-(.+)$") {
        return $Matches[1]
    }
    if ($value -match "^(.*)-$([regex]::Escape($CustomMark))\.(.+)$") {
        return $Matches[1]
    }
    return $null
}

function Get-VersionSortKey {
    param([string]$Raw)

    $parts = ($Raw -split '\.') | Where-Object { $_ -ne "" }
    return (($parts | ForEach-Object { '{0:D6}' -f [int]$_ }) -join '.')
}

function Compare-VersionStrings {
    param(
        [string]$Left,
        [string]$Right
    )

    $leftParts = @(($Left -split '\.') | ForEach-Object { [int]$_ })
    $rightParts = @(($Right -split '\.') | ForEach-Object { [int]$_ })
    $length = [Math]::Max($leftParts.Length, $rightParts.Length)

    for ($index = 0; $index -lt $length; $index++) {
        $leftValue = if ($index -lt $leftParts.Length) { $leftParts[$index] } else { 0 }
        $rightValue = if ($index -lt $rightParts.Length) { $rightParts[$index] } else { 0 }
        if ($leftValue -lt $rightValue) { return -1 }
        if ($leftValue -gt $rightValue) { return 1 }
    }

    return 0
}

function Increment-MinorVersion {
    param([string]$Raw)

    $parts = @($Raw -split '\.')
    $lastIndex = $parts.Length - 1
    $parts[$lastIndex] = ([int]$parts[$lastIndex] + 1).ToString()
    return ($parts -join '.')
}

function Increment-MajorVersion {
    param([string]$Raw)

    $parts = @($Raw -split '\.')
    $parts[0] = ([int]$parts[0] + 1).ToString()
    for ($index = 1; $index -lt $parts.Length; $index++) {
        $parts[$index] = "0"
    }
    return ($parts -join '.')
}

function Get-BumpMode {
    $raw = ($env:RELEASE_BUMP ?? $env:CUSTOM_BUMP ?? "auto").ToLowerInvariant()
    switch ($raw) {
        "auto" { return "auto" }
        "major" { return "major" }
        "minor" { return "minor" }
        "preserve" { return "preserve" }
        default { return "auto" }
    }
}

function Is-OfficialForkReleaseTag {
    param(
        [string]$TagName,
        [string]$CustomMark
    )

    if ($TagName -match '-build\.') {
        return $false
    }

    $customVersion = Extract-ForkVersion $TagName $CustomMark
    $baseVersion = Extract-BaseVersion $TagName $CustomMark
    if (-not $customVersion -or -not $baseVersion) {
        return $false
    }

    return [bool]($customVersion -match '^\d+(\.\d+)*$')
}

function Get-OfficialForkReleaseRecords {
    param(
        [string]$RefName,
        [string]$CustomMark
    )

    $records = @()
    $tags = git tag --merged $RefName --list "v*"
    foreach ($tagName in $tags) {
        $value = $tagName.Trim()
        if (-not $value) {
            continue
        }
        if (-not (Is-OfficialForkReleaseTag $value $CustomMark)) {
            continue
        }

        $records += [PSCustomObject]@{
            Tag           = $value
            CustomVersion = Extract-ForkVersion $value $CustomMark
            BaseVersion   = Extract-BaseVersion $value $CustomMark
            CustomSortKey = Get-VersionSortKey (Extract-ForkVersion $value $CustomMark)
            BaseSortKey   = Get-VersionSortKey (Extract-BaseVersion $value $CustomMark)
        }
    }

    return $records | Sort-Object CustomSortKey, BaseSortKey
}

function Get-LatestForkReleaseRecord {
    param([string]$CustomMark)

    $records = @(Get-OfficialForkReleaseRecords "HEAD" $CustomMark)
    if ($records.Length -eq 0) {
        return $null
    }
    return $records[-1]
}

function Has-CustomChangesSince {
    param(
        [string]$PreviousTag,
        [string]$UpstreamBranch
    )

    if (-not $PreviousTag) {
        return $false
    }

    $upstreamRef = "upstream/$UpstreamBranch"
    $upstreamExists = $false
    try {
        git rev-parse --verify $upstreamRef *> $null
        $upstreamExists = $true
    }
    catch {
        $upstreamExists = $false
    }

    if ($upstreamExists) {
        $commits = git log --no-merges --format=%H "$PreviousTag..HEAD" --not $upstreamRef
    }
    else {
        $commits = git log --no-merges --format=%H "$PreviousTag..HEAD"
    }

    return [bool](($commits | Where-Object { $_.Trim() -ne "" }) | Select-Object -First 1)
}

function Format-DisplayVersion {
    param(
        [string]$BaseVersion,
        [string]$CustomMark,
        [string]$CustomVersion
    )

    return "$BaseVersion-$CustomMark-$CustomVersion"
}

$rootDir = (git rev-parse --show-toplevel).Trim()
Push-Location $rootDir

try {
    $customMark = "wx"
    $customVersion = "1.0"
    $upstreamBranch = "main"
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
                "UPSTREAM_BRANCH" { $upstreamBranch = $parts[1] }
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

    if ($Mode -eq "snapshot" -or $Mode -eq "auto-release") {
        $baseTag = git tag --merged HEAD --list "v*" --sort=-version:refname |
            Where-Object { $_ -match $upstreamTagRegex } |
            Select-Object -First 1

        if (-not $baseTag) {
            throw "failed to resolve upstream base tag from current branch"
        }

        $baseVersion = $baseTag.Substring(1)
        $latestRecord = Get-LatestForkReleaseRecord $customMark
        $effectiveCustomVersion = $customVersion
        if ($latestRecord) {
            $bumpMode = Get-BumpMode
            switch ($bumpMode) {
                "major" {
                    $effectiveCustomVersion = Increment-MajorVersion $latestRecord.CustomVersion
                }
                "minor" {
                    $effectiveCustomVersion = Increment-MinorVersion $latestRecord.CustomVersion
                }
                "preserve" {
                    $effectiveCustomVersion = $latestRecord.CustomVersion
                }
                default {
                    if (Has-CustomChangesSince $latestRecord.Tag $upstreamBranch) {
                        $effectiveCustomVersion = Increment-MinorVersion $latestRecord.CustomVersion
                    }
                    else {
                        $effectiveCustomVersion = $latestRecord.CustomVersion
                    }
                }
            }
        }

        $displayVersion = Format-DisplayVersion $baseVersion $customMark $effectiveCustomVersion
        $version = $displayVersion
        $snapshotTag = "v$version"

        [PSCustomObject]@{
            MODE                     = $Mode
            BASE_TAG                 = $baseTag
            BASE_VERSION             = $baseVersion
            CUSTOM_MARK              = $customMark
            CUSTOM_VERSION           = $customVersion
            DISPLAY_VERSION          = $displayVersion
            EFFECTIVE_CUSTOM_VERSION = $effectiveCustomVersion
            RELEASE_TAG              = $snapshotTag
            RELEASE_NAME             = $version
            VERSION                  = $version
            SNAPSHOT_TAG             = $snapshotTag
            SNAPSHOT_NAME            = $version
            COMMIT                   = $shortCommit
            FULL_COMMIT              = $fullCommit
            BUILD_DATE               = $buildDate
            SOURCE_REPOSITORY        = $sourceRepository
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
        $version = Normalize-ReleaseVersion $Tag

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
