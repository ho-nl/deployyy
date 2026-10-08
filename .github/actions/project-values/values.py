"""The project's GitHub variables and secrets: masked, merged, exported.

See action.yml for the contract. Reads four JSON documents from the
environment, prints NAMES only, and writes two files in $RUNNER_TEMP that only
this job's user can read.
"""

import json
import os
import re
import shlex
import sys
import uuid

NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")

# Keys GitHub puts in a `secrets` context that are not the project's: the
# job's own token, and the one secret the hand-off travels in.
NOT_THE_PROJECTS = {"github_token", "all"}

# Names the runner itself depends on. They are still delivered (the platform
# decides what an app may receive); they are only kept out of the job's own
# environment, where they would break the steps after this one.
RUNNER_OWNED = re.compile(r"(PATH|HOME|(GITHUB|RUNNER|ACTIONS)_.*)", re.IGNORECASE)


def document(name):
    """One JSON object from the environment; anything else is no values."""
    raw = os.environ.get(name, "").strip()
    if not raw or raw == "null":
        return {}
    try:
        value = json.loads(raw)
    except ValueError:
        # Never print `raw`: it may be a secret.
        print(f"::warning::{name} is not JSON; ignored")
        return {}
    if not isinstance(value, dict):
        return {}
    return {k: v for k, v in value.items() if isinstance(v, str)}


def mask(value):
    """Mask a secret everywhere it could appear: per line, and JSON-escaped.

    A mask is one line: a multi-line value is masked line by line (a mask
    with a newline in it would print everything after the newline)."""
    seen = set()
    for candidate in (value, json.dumps(value)[1:-1]):
        for line in candidate.splitlines():
            line = line.strip("\r")
            # A line of only punctuation ("{", "},") reveals nothing, and
            # masking it turns every brace in the log into ***.
            if any(ch.isalnum() for ch in line) and line not in seen:
                seen.add(line)
                print(f"::add-mask::{line}")


caller_secrets = document("IN_SECRETS")
job_secrets = document("JOB_SECRETS")

# Mask FIRST, before anything below can print.
for source in (caller_secrets, job_secrets):
    for key, value in source.items():
        if key.lower() not in NOT_THE_PROJECTS:
            mask(value)

secrets = {}
skipped = set()
for source in (caller_secrets, job_secrets):  # the job's (Environment) wins
    for key, value in source.items():
        if key.lower() in NOT_THE_PROJECTS:
            continue
        if not NAME.fullmatch(key):
            skipped.add(key)
            continue
        secrets[key] = value

variables = {}
for source in (document("IN_VARS"), document("JOB_VARS")):  # the job's wins
    for key, value in source.items():
        if not NAME.fullmatch(key):
            skipped.add(key)
            continue
        variables[key] = value
# A name that is both is a secret: never show what might be one.
for key in secrets:
    variables.pop(key, None)

temp = os.environ.get("RUNNER_TEMP") or "/tmp"
values_path = os.path.join(temp, "deployyy-values.json")
env_path = os.path.join(temp, "deployyy-build-env")


def private(path):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    return os.fdopen(fd, "w")


with private(values_path) as out:
    json.dump({"variables": variables, "secrets": secrets}, out)

merged = {**variables, **secrets}
with private(env_path) as out:
    for key in sorted(merged):
        out.write(f"{key}={shlex.quote(merged[key])}\n")

exported = []
if os.environ.get("EXPORT", "true") == "true" and os.environ.get("GITHUB_ENV"):
    with open(os.environ["GITHUB_ENV"], "a") as out:
        for key in sorted(merged):
            if RUNNER_OWNED.fullmatch(key):
                continue
            delimiter = f"deployyy_{uuid.uuid4().hex}"
            while delimiter in merged[key]:
                delimiter = f"deployyy_{uuid.uuid4().hex}"
            out.write(f"{key}<<{delimiter}\n{merged[key]}\n{delimiter}\n")
            exported.append(key)

# A caller that passes `all` always hands over at least its github_token, so
# an empty one is a caller from before 2026-10-07: `secrets: inherit`, or
# COMPOSER_AUTH by name. It still builds with what the job sees; say what to
# change, because across organizations such a caller passes no secrets.
if not os.environ.get("IN_SECRETS", "").strip():
    print(
        "::warning title=Update the build workflow::This build was called the previous way "
        "(`secrets: inherit` or named secrets). Call it with `with: vars: ${{ toJSON(vars) }}` "
        "and `secrets: all: ${{ toJSON(secrets) }}`, and grant `permissions: contents: read, "
        "packages: write, actions: read, id-token: write` (README, Step 3). Until then only the "
        "secrets this job can see reach the build."
    )

print(f"variables: {', '.join(sorted(variables)) or 'none'}")
print(f"secrets:   {', '.join(sorted(secrets)) or 'none'} (values masked)")
if skipped:
    print(f"::warning::not valid as environment variable names, left out: {', '.join(sorted(skipped))}")
if exported:
    print(f"exported to this job's environment: {len(exported)}")

with open(os.environ.get("GITHUB_OUTPUT", os.devnull), "a") as out:
    print(f"build-env={env_path}", file=out)
    print(f"values={values_path}", file=out)

sys.exit(0)
