"""Release a template clone's first commit with the template's images.

A project cloned from a Deployyy template starts with one commit: the
template commit's tree, unchanged, with the trailer

    Deployyy-Template: <owner>/<repo>@<sha>

(deployyy-operator, the GitHubRepositoryBranchMigration copy). That commit
builds into the image the template already built for <sha>, as long as the
template built it with the same settings. So instead of building it again,
this copies the template's php-fpm and nginx images to the new commit's tags.

Every condition is checked, and any miss means "build as usual", never a
failed build:

  - the commit has no parent and carries the trailer;
  - its tree is the template commit's tree (the same files, byte for byte);
  - the application is the repository root (DEPLOYYY_ROOT_DIR unset);
  - the template's images for <sha> exist and this job can read them (the
    template's image package must be public);
  - their provenance names the template repository and <sha>, and the build
    arguments that shape the image (PLATFORM_LOCALES, PHP_VERSION) are what
    this build would pass.

Writes `released=true` to GITHUB_OUTPUT when it copied the images.
"""

import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request

TRAILER = re.compile(r"^Deployyy-Template:\s*([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)@([0-9a-f]{40})\s*$", re.M)

# The build arguments that change what the image holds. PLATFORM_OS_UPDATES_DAY
# only dates the nginx base layer's OS updates and is left out.
SHAPING_ARGS = ("PLATFORM_LOCALES", "PHP_VERSION")


def api(path):
    req = urllib.request.Request(
        os.environ.get("GITHUB_API_URL", "https://api.github.com") + path,
        headers={
            "Authorization": "Bearer " + os.environ["GH_TOKEN"],
            "Accept": "application/vnd.github+json",
        },
    )
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def template_of(commit):
    """(repo, sha) the commit was cloned from, or None."""
    if commit.get("parents"):
        return None
    m = TRAILER.search(commit["commit"]["message"])
    return (m.group(1), m.group(2)) if m else None


def build_args(provenance):
    """{name: value} of the build-arg:* parameters in a provenance document,
    plus vcs:source and vcs:revision. Accepts the per-platform map buildx
    prints for a multi-platform image as well as a single document."""
    found = {}

    def walk(o):
        if isinstance(o, dict):
            for k, v in o.items():
                if isinstance(v, str) and (k.startswith("build-arg:") or k in ("vcs:source", "vcs:revision")):
                    found.setdefault(k.removeprefix("build-arg:"), v)
                else:
                    walk(v)
        elif isinstance(o, list):
            for v in o:
                walk(v)

    walk(provenance)
    return found


def mismatch(args, repo, sha, want):
    """Why an image built with `args` is not the image this build would make,
    or "" when it is."""
    if args.get("vcs:revision") != sha:
        return f"it was built from {args.get('vcs:revision', 'an unknown commit')}, not {sha}"
    source = args.get("vcs:source", "").lower().removesuffix(".git")
    if source != f"https://github.com/{repo}".lower():
        return f"it was built from {source or 'an unknown repository'}, not {repo}"
    for name in SHAPING_ARGS:
        if args.get(name, "") != want.get(name, ""):
            return f"it was built with {name}={args.get(name, '')!r}, this build uses {want.get(name, '')!r}"
    return ""


def provenance(ref):
    out = subprocess.run(
        ["docker", "buildx", "imagetools", "inspect", ref, "--format", "{{json .Provenance}}"],
        capture_output=True, text=True,
    )
    if out.returncode != 0:
        return None
    try:
        return json.loads(out.stdout)
    except json.JSONDecodeError:
        return None


def main():
    def skip(why):
        print(f"Building: {why}")
        return 0

    if os.environ.get("DIRECTORY", ".") not in ("", "."):
        return skip("the application is in a folder, not the repository root.")
    repo, sha = os.environ["GITHUB_REPOSITORY"], os.environ["GITHUB_SHA"]
    try:
        commit = api(f"/repos/{repo}/commits/{sha}")
    except (urllib.error.URLError, KeyError) as e:
        return skip(f"could not read commit {sha[:7]} ({e}).")
    tpl = template_of(commit)
    if not tpl:
        return skip("this is not the first commit of a project cloned from a template.")
    tpl_repo, tpl_sha = tpl
    try:
        tpl_commit = api(f"/repos/{tpl_repo}/commits/{tpl_sha}")
    except urllib.error.URLError as e:
        return skip(f"could not read the template commit {tpl_repo}@{tpl_sha[:7]} ({e}).")
    if commit["commit"]["tree"]["sha"] != tpl_commit["commit"]["tree"]["sha"]:
        return skip(f"the files differ from the template commit {tpl_repo}@{tpl_sha[:7]}.")

    # Tags: php-fpm-[<php>-]<sha7>, the template's with its own sha.
    sha_tag = os.environ["SHA_TAG"]
    tpl_tag = sha_tag[:-7] + tpl_sha[:7]
    want = {"PLATFORM_LOCALES": os.environ["LOCALES"]}
    php_arg = os.environ.get("PHP_ARG", "")
    if php_arg.startswith("PHP_VERSION="):
        want["PHP_VERSION"] = php_arg.removeprefix("PHP_VERSION=")

    src = f"ghcr.io/{tpl_repo}".lower()
    dst = os.environ["IMAGE"].lower()
    for target in ("nginx", "php-fpm"):
        ref = f"{src}:{target}-{tpl_tag}"
        prov = provenance(ref)
        if prov is None:
            return skip(f"{ref} is not readable (it does not exist, or the template's image package is not public).")
        why = mismatch(build_args(prov), tpl_repo, tpl_sha, want)
        if why:
            return skip(f"{ref} differs: {why}.")

    # nginx first, php-fpm last: the operator deploys once both exist, the
    # same order as the build's own "Tag the release".
    for target in ("nginx", "php-fpm"):
        subprocess.run(
            ["docker", "buildx", "imagetools", "create", "-t", f"{dst}:{target}-{sha_tag}", f"{src}:{target}-{tpl_tag}"],
            check=True,
        )
    msg = (f"First commit of a project cloned from {tpl_repo}@{tpl_sha[:7]}, with the same files and settings. "
           f"Released {dst}:php-fpm-{sha_tag} and {dst}:nginx-{sha_tag} with the template's images.")
    print(msg)
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as f:
        print(msg, file=f)
    with open(os.environ["GITHUB_OUTPUT"], "a") as f:
        print("released=true", file=f)
    return 0


if __name__ == "__main__":
    sys.exit(main())
