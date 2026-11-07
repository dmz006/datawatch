Filed: https://github.com/dmz006/datawatch-app/issues/387

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.

Parent release: the datawatch release that added
`autonomous.inherited_tags_cache_seconds` — the daemon setting that controls
how long the shared inherited-tags map per Automaton is reused, so repeated
polling of the Automata list does not re-read the session archive.

## What changed in datawatch

Every Automaton has a set of tags its sessions inherit from it. The daemon
builds that map once and then, for as long as it is fresh, reuses it — that is
what keeps repeated polling of the Automata list from re-reading the session
archive every time. The new setting
`autonomous.inherited_tags_cache_seconds` (whole seconds) sets how long the
map is kept. It is hot-reloadable: a write applies to the running daemon
immediately, no restart.

- **0 (or unset)** → the default, **15 seconds**, is used.
- **A positive whole number** → that many seconds.
- **A negative value** → the cache is turned off and the map is rebuilt on
  every read.

The setting is an ordinary daemon config value under `autonomous`; nothing
else in the Automaton, session or tag objects changed.

## REST contract changed for the app

- `GET /api/config` — the `autonomous` object now returns
  `inherited_tags_cache_seconds` (a whole number of seconds). A value of 0
  means the 15-second default is in force; a negative value means the cache
  is off.
- `PUT /api/config` — new writable key
  `autonomous.inherited_tags_cache_seconds`. The daemon accepts any whole
  number: positive (lifetime in seconds), 0 (15-second default) or negative
  (cache off). The change is live on the next tag read, with no restart.
- The MCP autonomous config read and write, the CLI config surface and the
  chat `configure` command also read and set the same key — context only, no
  app work.

## Web Settings label

The Autonomous settings screen (the **Autonomous Automata planning** section)
has one new number field:

- Label (locale key `settings_inherited_tags_cache`):
  **Inherited tags cache (sec, 0=15, negative=off)**
- Help (locale key `settings_inherited_tags_cache_help`): "How long the
  shared cache of tags inherited from an Automaton is remembered before it is
  rebuilt. 0 uses the default (15 seconds); a negative value turns the cache
  off and rebuilds the index on every read."
- Placeholder value: `15`

## New locale keys

Both keys exist in all five web bundles (EN, DE, ES, FR, JA). Please add the
same two keys to the app's own string resources, using the values below:

| Key | EN | DE | ES | FR | JA |
|---|---|---|---|---|---|
| `settings_inherited_tags_cache` | Inherited tags cache (sec, 0=15, negative=off) | Cache für geerbte Tags (Sek., 0=15, negativ=aus) | Caché de tags heredados (seg, 0=15, negativo=desactivado) | Cache des tags hérités (sec, 0=15, négatif=désactivé) | 継承タグのキャッシュ（秒、0=15、負値=無効） |
| `settings_inherited_tags_cache_help` | How long the shared cache of tags inherited from an Automaton is remembered before it is rebuilt. 0 uses the default (15 seconds); a negative value turns the cache off and rebuilds the index on every read. | Wie lange die gemeinsame Cache der von einem Automaten geerbten Tags gespeichert bleibt, bevor sie neu aufgebaut wird. 0 nutzt den Standard (15 Sekunden); ein negativer Wert schaltet die Cache aus und baut den Index bei jedem Lesen neu. | Cuánto tiempo se recuerda la caché compartida de tags heredados de un autómata antes de reconstruirla. 0 usa el valor predeterminado (15 segundos); un valor negativo desactiva la caché y reconstruye el índice en cada lectura. | Durée pendant laquelle le cache partagé des tags hérités d'un automate est conservé avant d'être reconstruit. 0 utilise la valeur par défaut (15 secondes) ; une valeur négative désactive le cache et reconstruit l'index à chaque lecture. | オートマトンから継承したタグの共有キャッシュが再構築されるまでどれほど記憶されるかを指定します。0はデフォルト（15秒）を使います。負の値はキャッシュを無効にし、読み取りのたびにインデックスを再構築します。 |

(The web help text ends with one more sentence naming the docs link,
"…opens the Automata tags howto"; that is PWA-specific and may be dropped on
the app side.)

## Acceptance (app)

- The Autonomous settings screen has an **Inherited tags cache** number field
  whose label and help read as above, with the two locale keys present in all
  of the app's languages (EN, DE, ES, FR, JA).
- The app reads `autonomous.inherited_tags_cache_seconds` from the config
  read; a key that is absent or 0 shows the 15-second default, and a negative
  value reads as "cache off" rather than a negative lifetime.
- The app writes the key on save. Positive values, 0 and negative values are
  all accepted by the daemon, so the field must not clamp them to 1 or above;
  after saving, the change is in effect immediately with no daemon restart.
- **Capability required on Android and iOS:** the app can show, read and
  change `autonomous.inherited_tags_cache_seconds` in its Autonomous
  settings, on both platforms, the same way the web settings screen can —
  capability parity is required, implementation parity is not. A platform
  that cannot write the setting must say so explicitly instead of silently
  hiding the field.

## Filing record

- Filed: — (pending; issue number / URL once opened)
