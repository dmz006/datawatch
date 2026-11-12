import os
import unittest

import yaml

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
IDS = ["cve/CVE-2025-69720", "cve/CVE-2026-9538", "cve/CVE-2026-53615"]

WORDS = {
    "cve/CVE-2025-69720": ("infocmp", "ncurses-bin"),
    "cve/CVE-2026-9538": ("Archive::Tar", "git"),
    "cve/CVE-2026-53615": ("libblkid", "tmux"),
}


class TraceNcursesPerlUtilLinuxTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with open(os.path.join(ROOT, "security", "accepted-risks.yml")) as f:
            data = yaml.safe_load(f)
        cls.entries = {e["id"]: e for e in data["entries"]}
        with open(os.path.join(ROOT, ".trivyignore")) as f:
            cls.trivyignore = f.read()

    def test_traced(self):
        for i in IDS:
            imp = self.entries[i]["impact"]
            self.assertIs(imp["traced"], True, i)
            self.assertIn(imp["reachable"], ("yes", "no"), i)
            self.assertTrue(imp.get("method", "").strip(), i)
            self.assertGreaterEqual(len(imp["analysis"]), 300, i)
            self.assertNotIn("not independently traced", imp["analysis"], i)

    def test_evidence(self):
        for i in IDS:
            a = self.entries[i]["impact"]["analysis"]
            for word in WORDS[i]:
                self.assertIn(word.lower(), a.lower(), i)

    def test_still_suppressed(self):
        for i in IDS:
            self.assertIn(i.split("/", 1)[1], self.trivyignore, i)


if __name__ == "__main__":
    unittest.main()
