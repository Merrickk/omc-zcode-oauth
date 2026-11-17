#!/usr/bin/env python3
"""Generate and validate a plugin-store registry.json for this repository.

CPA's plugin store reads a registry document and installs each entry either from
the repository's GitHub releases (``github-release``) or from explicit artifact
URLs (``direct``). This script writes a document that satisfies CPA's own
validation rules, so a fork can publish its own source without hand-editing
JSON:

    python3 .github/scripts/make-registry.py \
        --repository https://github.com/<owner>/omc-zcode-oauth \
        --author "<owner>" --output registry/registry.json

Add ``--direct`` to emit a schema_version 2 document whose artifacts are the
archives passed with ``--artifact`` (``<goos>/<goarch>=<path>``); that variant
needs no GitHub API access at install time.
"""
import json
import re
from argparse import ArgumentParser
from hashlib import sha256
from pathlib import Path

PLUGIN_ID = "omc-zcode-oauth"
PLUGIN_NAME = "OMC ZCode OAuth"
ROOT = Path(__file__).resolve().parents[2]

ID_PATTERN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$")
VERSION_PATTERN = re.compile(r"^[0-9][0-9A-Za-z.+-]*$")
REPOSITORY_PATTERN = re.compile(r"^https://github\.com/([^/?#\s]+)/([^/?#\s]+)$")

DESCRIPTION = (
    "Native ZCode provider for CLIProxyAPI and Oh-My-CPA: dual-portal Z.ai / "
    "BigModel OAuth login, model discovery, Anthropic Messages execution, "
    "Coding Plan and gift routing, normalized quota, and a built-in gift-plan "
    "claim page."
)
DESCRIPTION_ZH = (
    "兼容 CLIProxyAPI 的原生 ZCode 提供商插件，支持 Z.ai / BigModel 双入口 OAuth 登录、"
    "模型发现、Anthropic Messages 请求、Coding Plan 与赠送额度路由、标准化配额展示，"
    "以及内置的活动领取页。"
)


def descriptor_version(root=ROOT):
    descriptor = json.loads((root / "go/assets/omc.json").read_text(encoding="utf-8"))
    version = descriptor["plugin_version"]
    if not VERSION_PATTERN.fullmatch(version) or version.startswith("v"):
        raise ValueError(f"invalid descriptor version {version!r}")
    return version


def check_repository(repository):
    match = REPOSITORY_PATTERN.fullmatch(repository)
    if not match:
        raise ValueError("repository must be https://github.com/{owner}/{repo}")
    if match.group(2).endswith(".git"):
        raise ValueError("repository must not end with .git")
    return match.group(1), match.group(2)


def check_version(version):
    if not VERSION_PATTERN.fullmatch(version) or version.startswith("v"):
        raise ValueError(f"invalid plugin version {version!r}")
    return version


def check_artifact(spec):
    """Parse ``<goos>/<goarch>=<path>`` and return one registry artifact entry."""
    try:
        platform, path = spec.split("=", 1)
        goos, goarch = platform.split("/", 1)
    except ValueError as error:
        raise ValueError(f"artifact {spec!r} is not <goos>/<goarch>=<path>") from error
    data = Path(path).read_bytes()
    if not data:
        raise ValueError(f"artifact {path} is empty")
    return {
        "goos": goos,
        "goarch": goarch,
        "url": "",  # filled by the caller: the registry must name an absolute URL
        "sha256": sha256(data).hexdigest(),
        "size": len(data),
    }


def build_entry(version, repository, author, artifacts=None):
    owner, _ = check_repository(repository)
    check_version(version)
    if not ID_PATTERN.fullmatch(PLUGIN_ID):
        raise ValueError(f"invalid plugin id {PLUGIN_ID!r}")
    entry = {
        "id": PLUGIN_ID,
        "name": PLUGIN_NAME,
        "description": DESCRIPTION,
        "author": author,
        "version": version,
        "repository": repository,
        "logo": f"https://raw.githubusercontent.com/{owner}/omc-zcode-oauth/main/assets/logo.png",
        "homepage": repository,
        "license": "MIT",
        "tags": ["Provider", "OAuth", "Quota", "GLM", "ZCode", "OMC"],
        "locales": {"zh-CN": {"description": DESCRIPTION_ZH}},
        "install": {"type": "direct" if artifacts else "github-release"},
    }
    if artifacts:
        entry["install"]["artifacts"] = artifacts
        entry["versions"] = [{"version": version, "install": {"type": "direct", "artifacts": artifacts}}]
    return entry


def build_registry(version, repository, author, artifacts=None):
    entry = build_entry(version, repository, author, artifacts)
    return {"schema_version": 2 if artifacts else 1, "plugins": [entry]}


def validate(document):
    """Mirror CPA's registry validation closely enough to fail before publishing."""
    if document.get("schema_version") not in (1, 2):
        raise ValueError("schema_version must be 1 or 2")
    plugins = document.get("plugins")
    if not isinstance(plugins, list) or not plugins:
        raise ValueError("plugins must be a nonempty list")
    seen = set()
    for index, plugin in enumerate(plugins):
        if plugin.get("id") in seen:
            raise ValueError(f"plugins[{index}]: duplicate plugin id {plugin.get('id')!r}")
        seen.add(plugin.get("id"))
        for field in ("id", "name", "description", "author"):
            if not str(plugin.get(field) or "").strip():
                raise ValueError(f"plugins[{index}]: missing required field {field}")
        if not ID_PATTERN.fullmatch(plugin["id"]):
            raise ValueError(f"plugins[{index}]: invalid plugin id {plugin['id']!r}")
        install_type = (plugin.get("install") or {}).get("type") or "github-release"
        if install_type == "github-release":
            check_repository(str(plugin.get("repository") or ""))
        elif install_type == "direct":
            if document["schema_version"] != 2:
                raise ValueError(f"plugins[{index}]: direct install requires schema_version 2")
            artifacts = (plugin.get("install") or {}).get("artifacts") or []
            if not artifacts:
                raise ValueError(f"plugins[{index}]: direct install requires artifacts")
            for position, artifact in enumerate(artifacts):
                where = f"plugins[{index}].artifacts[{position}]"
                if not artifact.get("goos") or not artifact.get("goarch"):
                    raise ValueError(f"{where}: missing goos or goarch")
                url = str(artifact.get("url") or "")
                if not url.startswith(("https://", "http://")):
                    raise ValueError(f"{where}: url must be absolute http(s)")
                digest = str(artifact.get("sha256") or "").lower()
                if not re.fullmatch(r"[0-9a-f]{64}", digest):
                    raise ValueError(f"{where}: invalid sha256")
        else:
            raise ValueError(f"plugins[{index}]: unsupported install type {install_type!r}")
        version = str(plugin.get("version") or "")
        if version:
            check_version(version)
    return document


def main():
    parser = ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True, help="https://github.com/{owner}/{repo}")
    parser.add_argument("--author", required=True, help="publisher name shown by the store")
    parser.add_argument("--version", default=None, help="defaults to go/assets/omc.json")
    parser.add_argument("--artifact", action="append", default=[], help="<goos>/<goarch>=<path> for a direct registry")
    parser.add_argument("--artifact-url", default="", help="base URL prefix for --artifact entries")
    parser.add_argument("--output", default="-", help="destination file, or - for stdout")
    args = parser.parse_args()

    version = check_version(args.version or descriptor_version())
    artifacts = None
    if args.artifact:
        if not args.artifact_url.startswith(("https://", "http://")):
            raise SystemExit("--artifact-url must be an absolute http(s) URL when --artifact is used")
        artifacts = []
        for spec in args.artifact:
            entry = check_artifact(spec)
            entry["url"] = args.artifact_url.rstrip("/") + "/" + Path(spec.split("=", 1)[1]).name
            artifacts.append(entry)

    document = validate(build_registry(version, args.repository, args.author, artifacts))
    text = json.dumps(document, ensure_ascii=False, indent=2, sort_keys=False) + "\n"
    if args.output == "-":
        print(text, end="")
        return
    path = Path(args.output)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")
    print(f"wrote {path} ({len(document['plugins'])} plugin entry, schema {document['schema_version']})")


if __name__ == "__main__":
    main()
