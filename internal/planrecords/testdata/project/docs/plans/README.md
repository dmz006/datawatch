# Plans registry (test fixture)

Items table, with a Depends on column.

| # | Item | Status | Depends on |
|---|---|---|---|
| 1 | [BL1](2026-01-01-alpha.md) Alpha feature works. More text follows in the same cell. | **done**, shipped in 9.1.0 (extra words) | - |
| 2 | **BL2** Beta thing | in progress | 1 |
| 3 | Gamma | wobbly | - |
| 3 | Gamma again, a repeated row number | planned | - |
| 4 | Alpha feature works | paused | - |
| 9 | | planned | - |

A second table with other column names and order.

| No | Status | Title | Note |
|---|---|---|---|
| 6 | blocked | Delta | waits for review |
| 7 | partial | Epsilon | - |
| 8 | in review | Zeta | - |
| 10 | Shipped 9.2.0 | Eta | - |

Rows with an id column and no number are not part of the numbered registry.

| B# | Item | Status | Found by |
|---|---|---|---|
| B1 | A defect row | done | test |
