"""Unit tests for config-parity-audit.py using tiny fixture surface sets."""

import contextlib
import importlib.util
import io
import os
import tempfile
import types
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))


def _load(name, filename):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, filename))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


audit = _load("config_parity_audit", "config-parity-audit.py")
surfaces = _load("config_parity_surfaces", "config_parity_surfaces.py")
covers = surfaces.covers


def full_sets(keys=("a.b",)):
    return {s: set(keys) for s in audit.SURFACES}


def run_main(keys, sets, exemptions_text=None, extra=()):
    """Run main() with fixture surfaces; return (code, stdout, stderr)."""
    fake = types.SimpleNamespace(
        covers=covers,
        web_settings_keys=lambda root: sets["web"],
        api_get_keys=lambda root: sets["get"],
        api_put_keys=lambda root: sets["put"],
        router_configure_keys=lambda root: sets["configure"],
        cli_config_set_keys=lambda root: sets["cli"],
    )
    out, err = io.StringIO(), io.StringIO()
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "ex.txt")
        with open(path, "w") as f:
            f.write(exemptions_text or "")
        argv = ["--exemptions", path] + list(extra)
        with mock.patch.object(audit, "list_keys", return_value=list(keys)), \
                mock.patch.object(audit, "load_surfaces_module", return_value=fake), \
                contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = audit.main(argv)
    return code, out.getvalue(), err.getvalue()


class CompareTests(unittest.TestCase):
    def test_key_present_everywhere_passes(self):
        rows, gaps, tg, te, stale = audit.compare(["a.b"], full_sets(), [], covers)
        self.assertEqual(tg, 0)
        self.assertEqual(te, 0)
        self.assertEqual(stale, [])
        self.assertTrue(all(v == "ok" for v in rows[0][1].values()))

    def test_missing_surface_is_reported(self):
        sets = full_sets()
        sets["put"] = set()
        rows, gaps, tg, _te, _st = audit.compare(["a.b"], sets, [], covers)
        self.assertEqual(tg, 1)
        self.assertEqual(gaps["put"], ["a.b"])
        self.assertEqual([s for s in audit.SURFACES if gaps[s]], ["put"])
        self.assertEqual(rows[0][1]["put"], "MISSING")

    def test_prefix_entry_covers_nested_key(self):
        sets = full_sets(["x.*"])
        _r, _g, tg, _te, _st = audit.compare(["x.y.z"], sets, [], covers)
        self.assertEqual(tg, 0)

    def test_exemption_with_reason_clears_gap(self):
        sets = full_sets()
        sets["cli"] = set()
        ex = [("a.b", {"cli"}, "not settable from the CLI", 1)]
        rows, gaps, tg, te, stale = audit.compare(["a.b"], sets, ex, covers)
        self.assertEqual((tg, te), (0, 1))
        self.assertEqual(rows[0][1]["cli"], "exempt")
        self.assertEqual(stale, [])

    def test_prefix_exemption_pattern(self):
        sets = full_sets()
        sets["web"] = set()
        ex = [("a.*", {"web"}, "reason", 3)]
        _r, _g, tg, te, _s = audit.compare(["a.b"], sets, ex, covers)
        self.assertEqual((tg, te), (0, 1))

    def test_stale_exemption_warns(self):
        ex = [("a.b", {"cli"}, "old reason", 7)]
        _r, _g, tg, _te, stale = audit.compare(["a.b"], full_sets(), ex, covers)
        self.assertEqual(tg, 0)
        self.assertEqual(len(stale), 1)
        self.assertIn("line 7", stale[0])


class ParseExemptionsTests(unittest.TestCase):
    def _parse(self, text):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "ex.txt")
            with open(p, "w") as f:
                f.write(text)
            return audit.parse_exemptions(p)

    def test_parses_entries_comments_and_wildcard(self):
        got = self._parse("# c\n\na.b | cli,web | why\nx.* | * | other\n")
        self.assertEqual(got[0], ("a.b", {"cli", "web"}, "why", 3))
        self.assertEqual(got[1][1], set(audit.SURFACES))

    def test_empty_reason_raises(self):
        with self.assertRaises(audit.AuditError):
            self._parse("a.b | cli | \n")

    def test_malformed_and_bad_surface_raise(self):
        with self.assertRaises(audit.AuditError):
            self._parse("a.b | cli\n")
        with self.assertRaises(audit.AuditError):
            self._parse("a.b | nope | why\n")

    def test_missing_file_raises(self):
        with self.assertRaises(audit.AuditError):
            audit.parse_exemptions("/nonexistent/ex.txt")


class ExitCodeTests(unittest.TestCase):
    def test_exit_0_when_all_present(self):
        code, out, _ = run_main(["a.b"], full_sets())
        self.assertEqual(code, 0)
        self.assertIn("total gaps: 0", out)

    def test_exit_0_when_gap_exempt(self):
        sets = full_sets()
        sets["get"] = set()
        code, _o, _e = run_main(["a.b"], sets, "a.b | get | write-only key\n")
        self.assertEqual(code, 0)

    def test_exit_1_on_unexempted_gap_names_surface(self):
        sets = full_sets()
        sets["configure"] = set()
        code, out, _ = run_main(["a.b"], sets)
        self.assertEqual(code, 1)
        self.assertIn("MISSING", out)
        self.assertIn("a.b", out)

    def test_exit_2_on_empty_reason(self):
        sets = full_sets()
        sets["get"] = set()
        code, _o, err = run_main(["a.b"], sets, "a.b | get | \n")
        self.assertEqual(code, 2)
        self.assertIn("empty reason", err)

    def test_exit_2_on_reader_failure(self):
        with mock.patch.object(audit, "list_keys", side_effect=audit.AuditError("boom")), \
                contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(audit.main(["--no-exemptions"]), 2)

    def test_exit_2_on_bad_usage(self):
        with contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(audit.main(["--bogus"]), 2)

    def test_stale_exemption_warns_but_passes(self):
        code, _o, err = run_main(["a.b"], full_sets(), "a.b | cli | stale\n")
        self.assertEqual(code, 0)
        self.assertIn("stale exemption", err)

    def test_json_output(self):
        sets = full_sets()
        sets["cli"] = set()
        code, out, _ = run_main(["a.b"], sets, extra=["--json"])
        self.assertEqual(code, 1)
        self.assertIn('"total_gaps": 1', out)


if __name__ == "__main__":
    unittest.main()
