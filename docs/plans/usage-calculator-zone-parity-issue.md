# datawatch-app issue: changed i18n keys for usage calculator reset zone parity

File this in **datawatch-app** (the mobile client is the source of truth for
translations). Title:

> feat(i18n): add changed keys for usage calculator reset zone parity

Per AGENT.md step 4 ("always file a datawatch-app issue"), body:

---

## (a) The changed keys + new EN values + context

Both keys already exist in the mobile locale bundles (introduced with the
v9.31.1 calculator settings card); this release **changes the English
values**, and the DE/ES/FR/JA bundles need to be re-mirrored from the new
English so all four languages carry the new meaning.

**Screen:** PWA Settings → General → "Usage calculator reset time zone"
field — the label above the input and the hint below it (mobile equivalents:
the settings screen that carries the usage calculator zone field).

| Key | New EN value |
|---|---|
| `settings_usage_calculator_reset_zone` | `Usage calculator reset time zone (IANA name; empty follows the server's zone)` |
| `settings_usage_calculator_reset_zone_hint` | `An IANA zone name such as America/New_York. Empty uses the zone the daemon runs in, falling back to UTC if that cannot be found. A fixed offset such as +02:00 is not accepted.` |

Old EN values (for the diff, from v9.31.1):

| Key | Old EN value |
|---|---|
| `settings_usage_calculator_reset_zone` | `Usage calculator reset time zone (IANA name, default UTC)` |
| `settings_usage_calculator_reset_zone_hint` | `An IANA zone name such as America/New_York. A fixed offset such as +02:00 is not accepted.` |

**Meaning to carry into the translations** (the whole point of the change):
an empty value no longer means a fixed default — it means *follow the zone
the daemon runs in*, resolved from the daemon's `TZ` environment variable,
then the system local-time zone, falling back to UTC when none resolves.
An explicit IANA name (a plain `UTC` included) always wins over the server's
zone and is never rewritten. Fixed offsets and `Local` are not accepted.

## (b) Parent release that introduced them

- Keys first shipped in **v9.31.1** (calculator settings card, locale keys
  added then with the old "default UTC" wording).
- The changed EN values above ship in the next minor release (pending bump
  to v9.32.0 in the parent repo, per the `BUMP=minor` note in that
  CHANGELOG entry).

## (c) Translation request

Please provide **DE, ES, FR and JA** translations of both keys through the
same Compose-Multiplatform pipeline, mirroring the parent's new English
wording for the *meaning* (empty = follows the daemon's zone; explicit
zone incl. `UTC` always wins; fixed offsets rejected) rather than
machine-translating the old strings. Keep `America/New_York` and `+02:00`
untranslated, as in the existing bundles.

---

## Filing notes (for the parent operator)

- Repo for the issue: `datawatch-app` (mobile client, source of truth for
  translations).
- Link this issue from the parent release notes of the bumping release.
- The parent's locale-guard test already covers the usage-calculator
  settings keys; no guard change needed in the parent for this parity task.
