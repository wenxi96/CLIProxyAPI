#!/usr/bin/env python3

import json
import mimetypes
import os
from pathlib import Path
import sys
import urllib.error
import urllib.parse
import urllib.request


class ApiError(RuntimeError):
    def __init__(self, status: int, message: str):
        super().__init__(f"GitHub API {status}: {message}")
        self.status = status
        self.message = message


REPO = os.environ["GITHUB_REPOSITORY"]
TOKEN = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
TAG = os.environ["RELEASE_TAG"]
NAME = os.environ.get("RELEASE_NAME") or TAG
TARGET = os.environ["RELEASE_TARGET_COMMIT"]
NOTES_FILE = Path(os.environ["RELEASE_NOTES_FILE"])
ASSET_PATHS = [Path(item) for item in sys.argv[1:]]

if not TOKEN:
    raise SystemExit("missing GH_TOKEN or GITHUB_TOKEN")

if not NOTES_FILE.exists():
    raise SystemExit(f"release notes file not found: {NOTES_FILE}")

for asset in ASSET_PATHS:
    if not asset.exists():
        raise SystemExit(f"release asset not found: {asset}")

API_ROOT = f"https://api.github.com/repos/{REPO}"
COMMON_HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": f"Bearer {TOKEN}",
    "User-Agent": "rebuild-release-history",
    "X-GitHub-Api-Version": "2022-11-28",
}


def api_json(url: str, method: str = "GET", payload: dict | None = None) -> tuple[int, dict]:
    data = None
    headers = dict(COMMON_HEADERS)
    if payload is not None:
        data = json.dumps(payload).encode("utf-8")
        headers["Content-Type"] = "application/json"

    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request) as response:
            body = response.read()
            return response.status, json.loads(body.decode("utf-8")) if body else {}
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        message = body
        try:
            payload = json.loads(body)
            message = payload.get("message") or body
        except json.JSONDecodeError:
            pass
        raise ApiError(exc.code, message) from exc


def api_empty(url: str, method: str = "DELETE") -> None:
    request = urllib.request.Request(url, headers=dict(COMMON_HEADERS), method=method)
    try:
        with urllib.request.urlopen(request):
            return
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        message = body
        try:
            payload = json.loads(body)
            message = payload.get("message") or body
        except json.JSONDecodeError:
            pass
        raise ApiError(exc.code, message) from exc


def get_release_by_tag(tag: str) -> dict | None:
    try:
        _, payload = api_json(f"{API_ROOT}/releases/tags/{urllib.parse.quote(tag, safe='')}")
        return payload
    except ApiError as exc:
        if exc.status == 404:
            return None
        raise


def list_assets(release_id: int) -> list[dict]:
    _, payload = api_json(f"{API_ROOT}/releases/{release_id}/assets")
    return payload if isinstance(payload, list) else []


def delete_asset(asset_id: int) -> None:
    api_empty(f"{API_ROOT}/releases/assets/{asset_id}")


def create_release() -> dict:
    payload = {
        "tag_name": TAG,
        "target_commitish": TARGET,
        "name": NAME,
        "body": NOTES_FILE.read_text(encoding="utf-8"),
        "draft": False,
        "prerelease": False,
    }
    _, release = api_json(f"{API_ROOT}/releases", method="POST", payload=payload)
    return release


def upload_asset(upload_url: str, asset_path: Path) -> None:
    asset_name = asset_path.name
    content_type = mimetypes.guess_type(asset_name)[0] or "application/octet-stream"
    target_url = f"{upload_url}?name={urllib.parse.quote(asset_name)}"
    headers = dict(COMMON_HEADERS)
    headers["Content-Type"] = content_type
    data = asset_path.read_bytes()
    request = urllib.request.Request(target_url, data=data, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(request):
            return
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        message = body
        try:
            payload = json.loads(body)
            message = payload.get("message") or body
        except json.JSONDecodeError:
            pass
        raise ApiError(exc.code, message) from exc


release = get_release_by_tag(TAG)
if release is None:
    release = create_release()
else:
    api_json(
        f"{API_ROOT}/releases/{release['id']}",
        method="PATCH",
        payload={
            "tag_name": TAG,
            "target_commitish": TARGET,
            "name": NAME,
            "body": NOTES_FILE.read_text(encoding="utf-8"),
            "draft": False,
            "prerelease": False,
        },
    )
    release = get_release_by_tag(TAG)
    if release is None:
        raise SystemExit(f"failed to reload release after update: {TAG}")

existing_assets = {asset["name"]: asset["id"] for asset in list_assets(release["id"])}
for asset in ASSET_PATHS:
    asset_id = existing_assets.get(asset.name)
    if asset_id is not None:
        delete_asset(asset_id)

upload_root = release["upload_url"].split("{", 1)[0]
for asset in ASSET_PATHS:
    print(f"Uploading {asset.name}")
    upload_asset(upload_root, asset)

print(f"Published {TAG}")
