#!/usr/bin/env python3

import json
import os
import sys
import urllib.error
import urllib.request


REPO = os.environ["GITHUB_REPOSITORY"]
TOKEN = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
TARGET_TAG = sys.argv[1]

if not TOKEN:
    raise SystemExit("missing GH_TOKEN or GITHUB_TOKEN")

API_ROOT = f"https://api.github.com/repos/{REPO}"
COMMON_HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": f"Bearer {TOKEN}",
    "User-Agent": "rebuild-release-history",
    "X-GitHub-Api-Version": "2022-11-28",
}


def request_json(url: str):
    request = urllib.request.Request(url, headers=COMMON_HEADERS)
    with urllib.request.urlopen(request) as response:
        return json.load(response)


def delete_release(release_id: int) -> None:
    request = urllib.request.Request(
        f"{API_ROOT}/releases/{release_id}",
        headers=COMMON_HEADERS,
        method="DELETE",
    )
    with urllib.request.urlopen(request):
        return


page = 1
deleted = False
while True:
    payload = request_json(f"{API_ROOT}/releases?per_page=100&page={page}")
    if not payload:
        break

    for release in payload:
        if release.get("tag_name") == TARGET_TAG:
            print(f"Deleting release id={release['id']} tag={TARGET_TAG}")
            delete_release(release["id"])
            deleted = True

    page += 1

if not deleted:
    print(f"No release found for tag {TARGET_TAG}")
