"""Deliver a branch's GitHub variables and secrets to Deployyy.

See action.yml. Never exits non-zero: a delivery that does not happen is a
warning, and the environment keeps the values it had.
"""

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

RETRY_STATUSES = {429, 502, 503, 504}
ATTEMPTS = 7  # about two minutes at 20s apart
WAIT = 20


def warn(message, step=""):
    # Deployyy's own sentences end in a full stop already; print one.
    message = message.rstrip(" .") + "."
    step = f" {step}" if step else ""
    print(f"::warning title=Variables not delivered::{message} The environment keeps its previous values.{step}")
    sys.exit(0)


def oidc_token(audience):
    request_url = os.environ.get("ACTIONS_ID_TOKEN_REQUEST_URL")
    request_token = os.environ.get("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
    if not request_url or not request_token:
        warn("The caller workflow does not grant `id-token: write`.",
             "Add `id-token: write` to the caller's `permissions`, see README Step 3.")
    separator = "&" if "?" in request_url else "?"
    request = urllib.request.Request(
        f"{request_url}{separator}audience={urllib.parse.quote(audience)}",
        headers={"Authorization": f"Bearer {request_token}"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        token = json.load(response)["value"]
    print(f"::add-mask::{token}")
    return token


def detail_of(body):
    try:
        problem = json.loads(body)
        return problem.get("refusal") or problem.get("detail") or problem.get("title") or ""
    except ValueError:
        return ""


def main():
    url = os.environ["URL"]
    audience = os.environ.get("AUDIENCE") or "deployyy"
    with open(os.environ["VALUES"]) as handle:
        values = json.load(handle)
    body = {"variables": values.get("variables", {}), "secrets": values.get("secrets", {})}
    if os.environ.get("PLATFORM"):
        body["platform"] = os.environ["PLATFORM"]
    # A repository with several applications: the folder says which project
    # this build was for ("" is the repository root, and is sent as such).
    if os.environ.get("SEVERAL") == "true":
        body["rootDirectory"] = os.environ.get("ROOT_DIRECTORY", "")
    payload = json.dumps(body).encode()

    for attempt in range(1, ATTEMPTS + 1):
        try:
            token = oidc_token(audience)
        except (urllib.error.URLError, KeyError, ValueError) as error:
            warn(f"GitHub identity token request failed: {error}.", "Re-run the build.")
        request = urllib.request.Request(
            url,
            data=payload,
            method="PUT",
            headers={
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
                "Accept": "application/json",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                answer = json.load(response)
        except urllib.error.HTTPError as error:
            text = error.read().decode(errors="replace")
            if error.code in RETRY_STATUSES and attempt < ATTEMPTS:
                print(f"Deployyy API returned HTTP {error.code}: {(detail_of(text) or 'not ready').rstrip(' .')}. Retrying in {WAIT}s.")
                time.sleep(WAIT)
                continue
            warn(f"Deployyy API returned HTTP {error.code}: {detail_of(text) or error.reason}.")
        except (urllib.error.URLError, TimeoutError) as error:
            if attempt < ATTEMPTS:
                print(f"Deployyy API unreachable: {error}. Retrying in {WAIT}s.")
                time.sleep(WAIT)
                continue
            warn(f"Deployyy API unreachable: {error}.", "Re-run the build.")

        lines = [answer.get("message") or "Variables and secrets delivered."]
        for label, key in (("Variables", "variables"), ("Secrets", "secrets"), ("Kept Deployyy values", "kept")):
            names = answer.get(key) or []
            if names:
                lines.append(f"{label}: {', '.join(names)}")
        for skipped in answer.get("skipped") or []:
            lines.append(f"Skipped {skipped.get('name')}: {skipped.get('reason')}")
        print("\n".join(lines))
        summary = os.environ.get("GITHUB_STEP_SUMMARY")
        if summary:
            with open(summary, "a") as out:
                out.write("### Variables and secrets\n\n" + "\n\n".join(lines) + "\n")
        return

    warn(f"Deployyy API did not accept the values after {ATTEMPTS} attempts.", "Re-run the build.")


if __name__ == "__main__":
    main()
