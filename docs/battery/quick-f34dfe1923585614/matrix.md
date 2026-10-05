# mischief battery — fault × detection × recovery matrix

- run id: `f34dfe1923585614`
- mode: --quick (parity matrix)
- generated: 2026-10-05T11:29:54Z
- catalog version: `d074ead2417d1b2cb812ed7e1e7c3a293eb9d15b03935062655304df203205ec`
- projects: scratch

no detector wired on this cell: the trouble-ledger integration is future work (the Detector seam is the AC-14 assertion point)

## Legacy parity (S-5)

The five bunker-qa.sh chaos cells this run replaces (deletion of the cells happens in a later milestone, after parity is proven live):

| legacy cell | replacing primitive | mapping |
|---|---|---|
| `chaos-disconnect` | `N-012` | dead-proxy network cut (HTTP(S)_PROXY→127.0.0.1:9) = the rate=1.0 end of N-012's userspace-proxy fault axis; the primitive adds the proxy-counter landed proof the cell never had |
| `chaos-shutdown` | `I-002` | compose stop + kill + restart = I-002's stop/kill/fresh-empty dependency lifecycle, with the external-probe + state-diff proof; tier L3 (docker API) — on an unprivileged L0 harness this cell records the named skip until the container backend lands |
| `chaos-corruption` | `F-009` | state-file truncate + restart + restore = F-009's replace-under-live-writer class (the 08-29/30 archive-hole shape), proven by inode identity instead of an unobserved rc |
| `chaos-resource` | `R-001` | suite under a 3G ulimit -v cap = R-001's memory-max scope squeeze with cgroup-counter proof (the cap cannot be dodged by a child re-exec) |
| `chaos-errorpath` | `F-009` | missing-config boot errorpath = F-009's unlink-then-create mode (the file a boot path needs is gone); shares the primitive with chaos-corruption because one fault class covers both cell shapes — recorded as the shared mapping, not a fifth primitive |

## Matrix

| cell | project | fault | legacy cell | verdict | detection | recovery |
|---|---|---|---|---|---|---|
| `q1` | scratch | `N-012` | `chaos-disconnect` | recovered | not_wired (no detector wired on this cell: the trouble-ledger integration is future work (…) | recovered |
| `q2` | scratch | `I-002` | `chaos-shutdown` | aborted | not_wired (no detector wired on this cell: the trouble-ledger integration is future work (…) | refused |
| `q3` | scratch | `F-009` | `chaos-corruption` | recovered | not_wired (no detector wired on this cell: the trouble-ledger integration is future work (…) | recovered |
| `q4` | scratch | `R-001` | `chaos-resource` | recovered | not_wired (no detector wired on this cell: the trouble-ledger integration is future work (…) | recovered |
| `q5` | scratch | `F-009` | `chaos-errorpath` | recovered | not_wired (no detector wired on this cell: the trouble-ledger integration is future work (…) | recovered |
