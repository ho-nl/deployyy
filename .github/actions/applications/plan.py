"""Which applications of the repository this workflow builds. See action.yml.

Writes two outputs: `build` (the build job's matrix) and `reuse` (the reuse
job's matrix), each a JSON list. Without DEPLOYYY_APPS the answer is one
entry, {"single": true, "label": "build"}: the build as it always was.
"""

import base64
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

ZERO = "0" * 40
# GitHub lists at most this many files in a comparison; a list that long may
# be cut, and a cut list cannot prove a folder unchanged.
MAX_FILES = 300
MANIFESTS = ", ".join([
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
    "application/vnd.docker.distribution.manifest.v2+json",
])
DIRECTORY = re.compile(r"^[A-Za-z0-9_.@+-]+(/[A-Za-z0-9_.@+-]+)*$")


def output(name, value):
    with open(os.environ["GITHUB_OUTPUT"], "a") as out:
        print(f"{name}={json.dumps(value, separators=(',', ':'))}", file=out)


def fail(message):
    print(f"::error title=DEPLOYYY_APPS::{message}")
    sys.exit(1)


def api(path):
    request = urllib.request.Request(
        os.environ.get("GITHUB_API_URL", "https://api.github.com") + path,
        headers={"Authorization": f"Bearer {os.environ['GH_TOKEN']}", "Accept": "application/vnd.github+json"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def changed_files():
    """The push's changed files, or None when they cannot be known in full."""
    before, after = os.environ.get("BEFORE", ""), os.environ["GITHUB_SHA"]
    if os.environ.get("EVENT") != "push":
        print("not a push: every application builds")
        return None
    if os.environ.get("ATTEMPT", "1") != "1":
        print("a re-run: every application builds")
        return None
    if os.environ.get("FORCED") == "true":
        print("a force push: every application builds")
        return None
    if not before or before == ZERO:
        print("a new branch: every application builds")
        return None
    try:
        answer = api(f"/repos/{os.environ['REPO']}/compare/{before}...{after}")
    except (urllib.error.URLError, OSError, ValueError) as error:
        print(f"::warning::Could not ask GitHub what this push changed ({error}); every application builds")
        return None
    files = answer.get("files") or []
    if answer.get("status") != "ahead" or len(files) >= MAX_FILES:
        print("GitHub cannot list this push's changes in full: every application builds")
        return None
    out = set()
    for f in files:
        out.add(f.get("filename", ""))
        if f.get("previous_filename"):
            out.add(f["previous_filename"])
    return out


def chosen_php(entry):
    php = entry.get("php") or {}
    if not isinstance(php, dict):
        return ""
    return (php.get("branches") or {}).get(os.environ["BRANCH"]) or php.get("default") or ""


def tags(entry, sha):
    """The release's tags for one commit, in push order: (tag, required)."""
    sha7 = sha[:7]
    if entry["application"] == "graphcommerce":
        return [(f"sha-{sha7}-cache-seed", False), (f"sha-{sha7}", True)]
    chosen = chosen_php(entry)
    tag = f"{chosen}-{sha7}" if chosen else sha7
    return [(f"nginx-{tag}", True), (f"php-fpm-{tag}", True)]


registry_tokens = {}


def tag_exists(image, tag):
    path = image.split("/", 1)[1]
    token = registry_tokens.get(path)
    if token is None:
        basic = base64.b64encode(f"x:{os.environ['GH_TOKEN']}".encode()).decode()
        query = urllib.parse.urlencode({"service": "ghcr.io", "scope": f"repository:{path}:pull"})
        request = urllib.request.Request(f"https://ghcr.io/token?{query}", headers={"Authorization": f"Basic {basic}"})
        with urllib.request.urlopen(request, timeout=30) as response:
            token = json.load(response)["token"]
        registry_tokens[path] = token
    request = urllib.request.Request(
        f"https://ghcr.io/v2/{path}/manifests/{tag}", method="HEAD",
        headers={"Authorization": f"Bearer {token}", "Accept": MANIFESTS},
    )
    try:
        with urllib.request.urlopen(request, timeout=30):
            return True
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return False
        raise


def main():
    try:
        variables = json.loads(os.environ.get("VARS") or "{}")
    except ValueError:
        variables = {}
    raw = (variables.get("DEPLOYYY_APPS") or "").strip() if isinstance(variables, dict) else ""
    if not raw:
        output("build", [{"single": True, "label": "build"}])
        output("reuse", [])
        return

    try:
        entries = json.loads(raw)
    except ValueError:
        fail(f"The repository variable DEPLOYYY_APPS is not JSON. The deployyy operator sets it; do not edit it by hand.")
    if not isinstance(entries, list):
        fail("The repository variable DEPLOYYY_APPS is not a list. The deployyy operator sets it; do not edit it by hand.")

    kind = os.environ["APPLICATION"]
    owner = os.environ["REPO"].split("/")[0].lower()
    mine = []
    for entry in entries:
        if not isinstance(entry, dict) or entry.get("application") != kind:
            continue
        directory = entry.get("directory") or ""
        if directory and (not DIRECTORY.match(directory) or any(p in (".", "..") for p in directory.split("/"))):
            fail(f"DEPLOYYY_APPS names the folder {directory!r}, which is not a path inside the repository")
        image = (entry.get("image") or f"ghcr.io/{os.environ['REPO']}").lower()
        if not image.startswith(f"ghcr.io/{owner}/"):
            fail(f"DEPLOYYY_APPS publishes {directory or 'the repository root'} to {image}; this build publishes to ghcr.io/{owner}/ only")
        mine.append({
            "directory": directory,
            "label": f"build ({directory or 'repository root'})",
            "image": image,
            "php": json.dumps(entry["php"], separators=(",", ":")) if entry.get("php") else "",
            "locales": entry.get("locales") or "",
            "gcMagentoEndpoints": json.dumps(entry["gcMagentoEndpoints"], separators=(",", ":")) if entry.get("gcMagentoEndpoints") else "",
            "_entry": entry,
        })
    if not mine:
        print(f"::notice::This repository holds no {kind} application Deployyy builds with this workflow: nothing to build.")
        output("build", [])
        output("reuse", [])
        return

    changed = changed_files() if any(app["directory"] for app in mine) else None
    build, reuse = [], []
    for app in mine:
        entry = app.pop("_entry")
        directory = app["directory"]
        if changed is not None and directory and not any(f.startswith(directory + "/") for f in changed):
            before = os.environ["BEFORE"]
            pairs = []
            try:
                for (old, required), (new, _) in zip(tags(entry, before), tags(entry, os.environ["GITHUB_SHA"])):
                    if tag_exists(app["image"], old):
                        pairs.append([old, new])
                    elif required:
                        pairs = None
                        break
            except (urllib.error.URLError, OSError, ValueError, KeyError) as error:
                print(f"::warning::Could not look up the release of {before[:7]} for {directory} ({error}); building it")
                pairs = None
            if pairs:
                print(f"{directory}: nothing in it changed since {before[:7]}, so its release is re-used")
                reuse.append({**app, "label": f"re-use ({directory})", "pairs": pairs, "from": before[:7]})
                continue
            print(f"{directory}: the release of {before[:7]} is not in the registry, so it builds")
        elif changed is not None and directory:
            print(f"{directory}: changed in this push, so it builds")
        build.append(app)
    output("build", build)
    output("reuse", reuse)


main()
