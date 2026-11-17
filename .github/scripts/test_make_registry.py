#!/usr/bin/env python3
"""Registry generation must satisfy the rules CPA's plugin store enforces."""
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "make-registry.py"
SPEC = importlib.util.spec_from_file_location("make_registry", SCRIPT)
make_registry = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(make_registry)

REPOSITORY = "https://github.com/example-owner/omc-zcode-oauth"


class BuildRegistryTest(unittest.TestCase):
    def test_release_registry_uses_the_descriptor_version(self):
        document = make_registry.build_registry(
            make_registry.descriptor_version(), REPOSITORY, "example-owner"
        )
        self.assertEqual(document["schema_version"], 1)
        plugin = document["plugins"][0]
        self.assertEqual(plugin["id"], "omc-zcode-oauth")
        self.assertEqual(plugin["install"], {"type": "github-release"})
        self.assertEqual(plugin["repository"], REPOSITORY)
        self.assertNotIn("versions", plugin)

    def test_direct_registry_pins_digests(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / "omc-zcode-oauth_0.3.0_linux_amd64.zip"
            archive.write_bytes(b"synthetic archive bytes")
            artifact = make_registry.check_artifact(f"linux/amd64={archive}")
        artifact["url"] = "https://example.com/omc-zcode-oauth_0.3.0_linux_amd64.zip"
        document = make_registry.build_registry("0.3.0", REPOSITORY, "example-owner", [artifact])
        self.assertEqual(document["schema_version"], 2)
        plugin = document["plugins"][0]
        self.assertEqual(plugin["install"]["type"], "direct")
        pinned = plugin["install"]["artifacts"][0]
        self.assertEqual(pinned["size"], len(b"synthetic archive bytes"))
        self.assertRegex(pinned["sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual(plugin["versions"][0]["install"]["artifacts"], [pinned])

    def test_generated_document_round_trips_through_json(self):
        document = make_registry.build_registry("0.3.0", REPOSITORY, "example-owner")
        text = json.dumps(document, ensure_ascii=False, indent=2) + "\n"
        self.assertEqual(make_registry.validate(json.loads(text)), document)


class RejectTest(unittest.TestCase):
    def test_repository_must_be_a_github_https_url(self):
        for repository in (
            "http://github.com/owner/repo",
            "https://gitlab.com/owner/repo",
            "https://github.com/owner",
            "https://github.com/owner/repo.git",
            "https://github.com/owner/repo?x=1",
        ):
            with self.subTest(repository=repository):
                with self.assertRaises(ValueError):
                    make_registry.build_registry("0.3.0", repository, "owner")

    def test_versions_reject_a_leading_v_and_empty_segments(self):
        for version in ("v0.3.0", "", "0.3.0/../x", " 0.3.0"):
            with self.subTest(version=version):
                with self.assertRaises(ValueError):
                    make_registry.check_version(version)

    def test_direct_requires_schema_two_and_absolute_urls(self):
        plugin = {
            "id": "omc-zcode-oauth",
            "name": "n",
            "description": "d",
            "author": "a",
            "version": "0.3.0",
            "install": {"type": "direct", "artifacts": [
                {"goos": "linux", "goarch": "amd64", "url": "/relative.zip", "sha256": "0" * 64}
            ]},
        }
        with self.assertRaises(ValueError):
            make_registry.validate({"schema_version": 2, "plugins": [plugin]})
        plugin["install"]["artifacts"][0]["url"] = "https://example.com/a.zip"
        with self.assertRaises(ValueError):
            make_registry.validate({"schema_version": 1, "plugins": [plugin]})
        make_registry.validate({"schema_version": 2, "plugins": [plugin]})

    def test_duplicate_ids_and_missing_fields_are_refused(self):
        document = make_registry.build_registry("0.3.0", REPOSITORY, "example-owner")
        duplicated = {"schema_version": 1, "plugins": document["plugins"] * 2}
        with self.assertRaises(ValueError):
            make_registry.validate(duplicated)
        for field in ("id", "name", "description", "author"):
            broken = json.loads(json.dumps(document))
            broken["plugins"][0][field] = " "
            with self.subTest(field=field):
                with self.assertRaises(ValueError):
                    make_registry.validate(broken)


if __name__ == "__main__":
    unittest.main()
