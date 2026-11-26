# mischief docs gap audit — frozen source/runtime transcript

- target: /home/kara/mischief
- published ref: origin/main
- audited SHA: 456c6d4374995773854a27ed2476251a5c04b6aa
- all commands below run from /home/kara/mischief unless stated

## COMMAND
`git rev-parse HEAD`

456c6d4374995773854a27ed2476251a5c04b6aa

EXIT: 0

## COMMAND
`git status --short`


EXIT: 0

## COMMAND
`make bin`

toolchain: go1.26.9 from the module cache at /mnt/ssd/caches/hermes-go-mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go (satisfies go.mod's toolchain go1.26.9; downloaded under GOMODCACHE=/mnt/ssd/caches/hermes-go-mod)
bin: bin/mischief (mischief 456c6d437499, toolchain go1.26.9)

EXIT: 0

## COMMAND
`./bin/mischief --help`

mischief — fault injection for a fleet of Linux daemons (M1 chassis)

Usage:
  mischief plan      -f <exp.yaml> [--json] [--scratch-dir D] [--journal-dir D]  (writes nothing without --journal-dir)
  mischief status    [--dir D] [--all] [--json]  (active holds: armed/stuck; a revert_failed fault reads as exit 1 + a counter; --all lists terminal holds too)
  mischief arm       --fault ID --target SEL --ttl D --inverse-kind K --inverse-params k=v,.. --check-kind K --check-params k=v,.. [--dir D]
  mischief land      (--hold <id> | --all) [--dir D]  (record that YOU landed the fault; arm is write-ahead only)
  mischief revert    (--all | --hold <id> [--retry]) [--dir D]
  mischief doctor    [--catalog-dir D] [--json] [--self-check]  (exit 1 = a check FAILED: catalog load, or a --self-check rail)
  mischief selftest  (--all | --primitive <id>) [--dir D] [--catalog-dir D]  (exit 0 only when green-or-skip — the M1 exit criterion)
  mischief serve     --dir D          (reverter daemon: TTLs + boot reconcile + health.json)
  mischief install   [--user U] [--prefix P] [--dry-run] [--no-drop-in]
  mischief uninstall [--prefix P] [--dry-run] [--keep-count N] [--keep-days D]
  mischief audit     [--prefix P] [--json]
  mischief retention [--apply] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]
  mischief battery   [--quick] [--project P] [--primitive ID] [--out D] [--file-rows] [--dry-run]
  mischief chaos     matrix --project P [--tier-max N] [--out D] | check --lane F | finding --project P --board D --title T --reason R
  mischief sim       --shape I-00N [--path P] [--log F] [--allow-real --resource TAG --spend-cap USD]
  mischief profile   run <class>/<name> --target <selector> [--json] [--seed N] [--dir D]  (applies the profile and grades it: exit 0 passed / 1 failed / 3 inconclusive)
  mischief profile   list [--profiles-dir D]
  mischief plane     parse-agent-id | record | selftest-verdict | fold   (bunker test-plane evidence CLI)
  mischief version   [--json]

Safety floor: no default target; protected targets refuse at resolution;
sanction-marker gate (SPEC-13) fail-closed on the landing verbs (plan, arm,
selftest, battery, sim, profile — unsanctioned hosts refuse exit 2); chaos matrix
is evidence-plane and stamps its host posture into the artifact; load gate (AC-8,
loadavg 12.0 / PSI io 50.0 — the doctor's run thresholds) fail-closed on the
fault-landing verbs (selftest, battery, sim — a saturated host refuses exit 2
with the measured numbers against the gate, nothing lands). profile run is
sanction-gated but NOT load-gated in this build: it names that gap in the
finding's notes rather than implying the gate ran.

EXIT: 0

## COMMAND
`./bin/mischief profile --help`

mischief profile — apply a testing profile and grade the target

Usage:
  mischief profile run  <class>/<name> --target <selector> [--json] [--seed N] [--dir D] [--profiles-dir D] [--catalog-dir D]
  mischief profile list [--profiles-dir D]
  mischief profile compare <run-dir-a> <run-dir-b> [--json]

run loads catalog/profiles/<class>/<name>.yaml, resolves every step's fault
against catalog/faults/, applies each step in order, runs the step's probe and
writes a graded finding to stdout plus a run dir (<dir>/<run_id>/).

compare reads the two run dirs' journals (profile.jsonl), pairs steps by id
between two runs of the SAME profile, and reports the outcome transition per
probe: failed->passed (the fix landed), passed->failed (a regression),
unchanged. Run the same profile twice with the same --seed and DIFFERENT --dir
roots (a run dir is content-addressed, so re-running into one --dir replaces
the previous run's journal), then hand the two run dirs to compare.

Exit codes (the graded outcome):
  0  passed        the fault landed and the probe held
  1  failed        the fault landed and the probe broke
  3  inconclusive  no signal: a missing primitive, an unmet requirement, no
                   actuator, or a target this build cannot attach to
  2  refused       usage, sanction, protected target or missing profile —
                   nothing was landed

--target takes the same selector grammar as arm/plan. scratch:<path> is the
only kind this build can fault (the executor spawns its own scenario writer
under an LD_PRELOAD shim); any other kind grades inconclusive.

EXIT: 0

## COMMAND
`./bin/mischief version --json`

{"version":"456c6d437499","tool":"mischief"}

EXIT: 0

## COMMAND
`./bin/mischief version --version`

mischief 456c6d437499

EXIT: 0

## COMMAND
`./bin/mischief version --nope; echo command_rc=$?`

version: unknown flag --nope; supported: --json, --version
command_rc=2

EXIT: 0

## COMMAND
`grep -n '^version' skills/mischief-usage/SKILL.md`

50:version                      [--json]                              # stamped git sha (tag on release builds); --json emits {"version":..,"tool":"mischief"}

EXIT: 0

## COMMAND
`grep -n '^| ' docs/OPERATIONS.md`

9:| unit | binary | surface |
11:| `mischief` | one static Go binary (`make bin`, `.goreleaser.yml` for releases) | CLI: `plan`, `arm`, `land`, `status`, `revert`, `doctor`, `selftest`, `install`, `uninstall`, `audit`, `retention`, `battery`, `chaos`, `sim`, `plane` |
12:| `mischiefd` | `mischief serve` | reverter daemon: owns TTLs, reconciles previous-boot leftovers, writes `health.json`, and (since MISCHIEF-OPS-001) serves Prometheus metrics |
89:| key | meaning |
91:| `ok` | `true` while the daemon folds state and no hold sits in the failed terminal state; stuck leftovers from a previous boot do NOT flip it (they are a reported condition, not a daemon defect) |
92:| `daemon_boot` | THIS daemon process's boot id (hex16), minted fresh at each start — key on it to tell a stale file from the live daemon's; it is NOT the reverter fold's host-boot-scoped id |
93:| `pid` | the daemon process's pid |
94:| `run_dir` | the absolute run dir (the journal's home) |
95:| `started_at` | the daemon's start time, unix seconds |
96:| `generated_at` | this document's write time, unix seconds — the STALENESS datum: the heartbeat is 5 s, so compare it against `date +%s`; a delta of more than a couple of beats means the daemon stopped beating and `ok:true` then says nothing about the run |
97:| `holds` | the COMPLETE listing: one row per known hold, terminal rows included (see the surface warning below) |
98:| `stuck` | count of holds that boot reconcile marked stuck (previous boots' leftovers) |
99:| `reverted` | count of holds with a measured revert proof |
100:| `revert_failed` | count of holds whose measured revert FAILED — the fault may still be live, and this is the one hold state that flips `ok` false |
101:| `not_landed` | count of holds whose proof recorded that the fault was NEVER landed: not a pass and not a failure (no fault was applied, so the run carries no integrity signal) |
102:| `refold_error` | the journal re-fold error string, present ONLY when non-empty (`omitempty`): the document then carries the LAST KNOWN fold with `ok:false`, never a silent stale view |
113:| key | meaning |
115:| `hold_id` | the hold's stable id |
116:| `fault_id` | the catalog fault the hold was armed for (e.g. `S-001`) |
117:| `target` | the target selector the hold was armed on (e.g. `scratch:/path/to/target`) |
118:| `status` | the hold's state: `armed`, `stuck`, `reverted`, `revert_failed`, or `not_landed` |
119:| `ttl_remaining_sec` | float seconds left before the sweep reverts the hold — ARMED ROWS ONLY (`omitempty`) |
120:| `stuck_at` | unix seconds at which boot reconcile marked the hold stuck — STUCK ROWS ONLY (`omitempty`) |
185:| env var | default | meaning |
187:| `MISCHIEFD_METRICS_ADDR` | `127.0.0.1:9464` | listen address for `GET /metrics`; `off` (or `disabled`) turns the surface off |
199:| metric | type | meaning |
201:| `mischiefd_up` | gauge | 1 while the exposition answers |
202:| `mischiefd_start_time_seconds` | gauge | process start (unix seconds) |
203:| `mischiefd_cycles_total` | counter | completed heartbeat/sweep cycles |
204:| `mischiefd_scrapes_total` | counter | scrapes served |
205:| `mischiefd_revert_total{result}` | counter | revert attempts by the journal's closed outcome vocabulary: `no_hold`, `not_landed`, `revert_failed`, `reverted` (`no_hold` means the hold vanished between snapshot and revert — reported, not fatal) |
206:| `mischiefd_errors_total{reason}` | counter | daemon error paths: `refold` (Journal corruption held for the render — DEGRADED, the loop survives), `revert` (a revert attempt failed fatally), `health_write` (health.json publish failed) |
207:| `mischiefd_revert_duration_seconds` | histogram | one revert attempt end to end (action + measured check), buckets 5 ms … 10 s |
281:| field | type | presence | meaning |
283:| `type` | string, closed set of ten | always | the record type (vocabulary below). The write path refuses an unknown type and the read path fails loudly on one rather than folding it into a summary. |
284:| `run_id` | string | always | the content-derived run id, 16 hex chars: `hex(sha256("seed\n<seed>\n" + spec_bytes))[:8]` (`RunID`, `internal/journal/experiment.go:419`). Scrubbed on write like any free-form field. |
285:| `seq` | int | always | the run-local sequence number: 0-based, monotonic in append order |
286:| `ts` | int64 | always | unix timestamp, seconds |
287:| `verdict` | string, closed vocabulary | only on `verdict` and `run_closed` records | the graded verdict value: `recovered`, `degraded`, `not_recovered`, `hung`, `corrupted`, `no_op`, `aborted`, `flaky` (PRD §6.4). Write validation refuses a verdict on any other record type. |
288:| `fields` | object (string → string) | optional, omitted when empty | record-specific detail. Every value is a JSON string — never a number; numeric-looking values ride as strings (see the example below). Values are scrubbed on write, and the fully serialised line gets a byte-level scrub backstop. |
295:| `type` | written when |
297:| `run_planned` | the resolved plan before any side effect (AC-1): once per `plan --journal-dir D`, and as the opening record of the `selftest` and `battery` journals |
298:| `fault_armed` | the fault actuator is armed, pre-land (`selftest`) |
299:| `fault_landed` | carrying the landing proof (`selftest`, `battery`) |
300:| `probe_sample` | one probe observation — declared in the vocabulary; no writer yet in the current tree |
301:| `assertion_result` | one probe/assertion grading — declared in the vocabulary; no writer yet in the current tree |
302:| `observe_result` | the observe.trouble outcome (AC-14) — the record builder lives in `internal/observe`; wiring a verb to it is tracked as future (`internal/battery/matrix.go`) |
303:| `fault_reverted` | carrying the revert proof (AC-4) (`selftest`, `battery`) |
304:| `verdict` | the graded verdict value (AC-9): appended by the `selftest` and `battery` graders and rendered by the verdict engine (`Result.Record`, `internal/verdict/engine.go:379`) |
305:| `run_closed` | the run's terminal record, carrying the same verdict (`selftest`, `battery`) |
306:| `finding` | a finding filed from the run (AC-18 shape) — declared in the vocabulary; no writer yet in the current tree |
323:| symptom | first probe |
325:| `ok:false` in health.json | `jq .refold_error,.revert_failed health.json`; a `refold_error` is corruption, a `revert_failed` is a live fault |
326:| metrics port silent, daemon alive | check the unit's `MISCHIEFD_METRICS_ADDR`; a bind failure is logged to stderr and is non-fatal by design |
327:| `mischiefd_cycles_total` flat | the sweep is not running: read the daemon's last stderr lines (a fatal revert error exits the loop) |
328:| stale health.json | compare `daemon_boot`/`pid` against the running process; a stale file after a restart means the supervisor did not start the daemon |

EXIT: 0

## COMMAND
`grep -n 'profile\|version' docs/OPERATIONS.md`

315:{"type":"run_planned","run_id":"84c8a84cecf315c1","seq":0,"ts":1795040158,"fields":{"catalog_version":"6ba6eda1a66b080788f89193011d5ee45e33281895553748b620a7dddf999556","experiment":"ex-001-noop-detector","faults":"F-008","faults_count":"1","kind":"plan","mode":"preview","refusals":"0","seed":"20260927","target_kind":"host","target_selector":"host:."}}

EXIT: 0

## COMMAND
`grep -n 'profile\|version' README.md`

47:| `cmd/mischief/` + `Makefile` | the M1 chassis CLI (`make bin`) — core verbs: `plan` (zero side effects, AC-1: nothing is written; `--journal-dir D` opts in to recording the preview as `D/plan.jsonl`) · `status` (default: ACTIVE holds; a terminal `revert_failed` hold reads as exit 1 + a `revert_failed: N` counter in both modes — the same condition the daemon grades `ok:false`; `--all` also lists terminal `reverted` / `revert_failed` holds) · `land` (`--hold <id>` \| `--all`) records that YOU landed an armed hold's fault — the LAND step of the loop, which had no verb; it records the caller's claim and proves nothing · `revert` (`--all` is the kill switch; a hold with no landed record grades `not_landed` and exits 3 rather than reporting a clean sweep; `--hold <id> --retry` re-runs a terminal `revert_failed` hold's recorded inverse as one more measured attempt) · `doctor` (rails self-checks) · `selftest --all \| --primitive <id>` (L0 scratch land+revert proofs per primitive; exit 0 only green-or-skip — the M1 exit criterion, AC-19/AC-3/AC-4) · `serve` (reverter daemon: TTLs, boot reconcile, `health.json`) · `version` (stamped git sha); v0.1 also ships `install` (privileged helper manifest + a visudo-validated sudoers drop-in — never elevates itself) · `uninstall` · `audit` (survey the live privileged surface against the canonical verb table) · `retention` (rotate old run dirs; report-only without `--apply`), plus the evidence verbs `battery` · `chaos` · `sim` · `plane`. Battery honesty: the matrix schema is fault × detection × recovery, but the detection axis is NOT measured in v0.1 — no production detector is wired (only `detected (observer)` rows are detection claims; `gap`/`not_wired`/`error` mean detection was not measured; see docs/BATTERY-PARITY.md) |
60:Short version: download, verify, unpack, then `mkdir -p ~/.local/bin && install -m 0755 mischief ~/.local/bin/`.
93:mischief version   # mischief v0.1.0 (a release stamps the tag; `make bin` stamps the git sha)
212:stops BEFORE compiling and names the requirement — the exact version, what it found,
220:make bin           # → bin/mischief, CGO_ENABLED=0, version stamped with the git sha
221:bin/mischief version          # mischief <short-sha of HEAD>
222:bin/mischief version --json   # {"version":"<short-sha>","tool":"mischief"}

EXIT: 0

## COMMAND
`grep -n 'profile run\|profile list\|mischief arm\|mischief land' docs/prd/mischief-v0.1.md`


EXIT: 1

## COMMAND
`grep -n 'reverter.jsonl\|land_refused\|hold_expired\|hold_stuck' docs/OPERATIONS.md`


EXIT: 1

## COMMAND
`grep -RInE 'os\.(Getenv|LookupEnv)|fs\.(Bool|String|Int|Duration|Float64)' cmd internal --include='*.go'`

cmd/mischief/plane.go:68:	in := fs.String("in", "-", "read spawn output from this file, - for stdin")
cmd/mischief/plane.go:97:	file := fs.String("file", "", "evidence JSONL file (required)")
cmd/mischief/plane.go:98:	step := fs.String("step", "", "pipeline stage: preflight | build | spawn | ship | selftest | collect | destroy (required)")
cmd/mischief/plane.go:99:	status := fs.String("status", "", "pass | fail | skip (required)")
cmd/mischief/plane.go:100:	detail := fs.String("detail", "", "reason/proof text (encoded by this verb)")
cmd/mischief/plane.go:147:	code := fs.Int("exit-code", -1, "the remote selftest's exit code (required)")
cmd/mischief/plane.go:162:	file := fs.String("file", "", "evidence JSONL file (required; - reads stdin)")
cmd/mischief/arm.go:55:	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
cmd/mischief/arm.go:56:	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
cmd/mischief/arm.go:57:	fault := fs.String("fault", "", "fault id to arm a hold for (required)")
cmd/mischief/arm.go:58:	target := fs.String("target", "", "target selector the fault lands on (required)")
cmd/mischief/arm.go:59:	ttl := fs.Duration("ttl", 0, "how long the fault may be held (required, e.g. 30s)")
cmd/mischief/arm.go:60:	invKind := fs.String("inverse-kind", "", "inverse action kind: restore-file | remove-file (required)")
cmd/mischief/arm.go:61:	invParams := fs.String("inverse-params", "", "inverse params, k=v[,k=v...] (e.g. path=/tmp/f,backup=/tmp/f.bak)")
cmd/mischief/arm.go:62:	chkKind := fs.String("check-kind", "", "measured post-revert check kind: file-matches-backup | file-absent (required)")
cmd/mischief/arm.go:63:	chkParams := fs.String("check-params", "", "check params, k=v[,k=v...]")
cmd/mischief/plan.go:48:	expPath := fs.String("f", "", "experiment YAML (required)")
cmd/mischief/plan.go:49:	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
cmd/mischief/plan.go:50:	asJSON := fs.Bool("json", false, "emit JSON instead of text")
cmd/mischief/plan.go:57:	scratchDir := fs.String("scratch-dir", "", "scratch dir the plan declares for the L0 preview (verbatim, never created by plan; run owns the flag)")
cmd/mischief/plan.go:61:	journalDir := fs.String("journal-dir", "", "run dir to RECORD the plan preview into (<dir>/plan.jsonl); default: none — plan writes nothing (AC-1)")
cmd/mischief/plan.go:62:	skipSanction := fs.Bool("plan-only-sanction-skip", false, "(unimplemented stub flag: reserved for MSF-020; plan never lands faults)")
cmd/mischief/ops.go:42:	user := fs.String("user", "", "sudoers grantee account (default: mischief)")
cmd/mischief/ops.go:43:	prefix := fs.String("prefix", ops.DefaultPrefix, "mischief state prefix")
cmd/mischief/ops.go:44:	dryRun := fs.Bool("dry-run", false, "print the exact plan; touch nothing")
cmd/mischief/ops.go:45:	noDropIn := fs.Bool("no-drop-in", false, "plan/install the manifest only (the drop-in needs root)")
cmd/mischief/ops.go:95:	prefix := fs.String("prefix", ops.DefaultPrefix, "mischief state prefix")
cmd/mischief/ops.go:96:	runsDir := fs.String("runs-dir", "", "journal runs dir to expire per the retention policy (empty: derive under $HOME)")
cmd/mischief/ops.go:97:	keepCount := fs.String("keep-count", "", "retention: keep at most N newest run dirs")
cmd/mischief/ops.go:98:	keepDays := fs.String("keep-days", "", "retention: expire run dirs older than D days")
cmd/mischief/ops.go:99:	dryRun := fs.Bool("dry-run", false, "print the exact plan; touch nothing")
cmd/mischief/ops.go:144:	prefix := fs.String("prefix", ops.DefaultPrefix, "mischief state prefix to survey")
cmd/mischief/ops.go:145:	user := fs.String("user", "", "sudoers grantee the expected drop-in was rendered for (default: mischief)")
cmd/mischief/ops.go:146:	asJSON := fs.Bool("json", false, "emit JSON (the operator diff surface)")
cmd/mischief/ops.go:210:	apply := fs.Bool("apply", false, "apply the rotation (default: report only)")
cmd/mischief/ops.go:211:	runsDir := fs.String("runs-dir", "", "journal runs dir (default: derived under $HOME)")
cmd/mischief/ops.go:212:	keepCount := fs.String("keep-count", "", "keep at most N newest run dirs (default: 20)")
cmd/mischief/ops.go:213:	keepDays := fs.String("keep-days", "", "expire run dirs older than D days (default: 30)")
cmd/mischief/ops.go:214:	dryRun := fs.Bool("dry-run", false, "print the exact plan; touch nothing")
cmd/mischief/sim.go:100:	shapeID := fs.String("shape", "", "the catalog id to simulate (I-003..I-014 or S-010)")
cmd/mischief/sim.go:101:	path := fs.String("path", "/fault", "request path the sim serves")
cmd/mischief/sim.go:102:	logPath := fs.String("log", "", "sim request log path (default <tmpdir>/mischief-sim-<shape>.log)")
cmd/mischief/sim.go:103:	destroyClass := fs.String("destroy-class", "reversible", "the requested fault's destroy class, closed vocabulary reversible | permanent (permanent refuses outright in v0.1)")
cmd/mischief/sim.go:104:	allowReal := fs.Bool("allow-real", false, "AC-17 opt-in: admit the real-provider plane (v0.1: gate decides; adapter is a stub)")
cmd/mischief/sim.go:105:	resource := fs.String("resource", "", "allowlisted resource tag (AC-17 triad)")
cmd/mischief/sim.go:106:	spendCap := fs.Float64("spend-cap", 0, "spend cap in USD (AC-17 triad)")
cmd/mischief/profile.go:92:	profilesDir := fs.String("profiles-dir", "", "catalog/profiles dir (default: resolved from cwd)")
cmd/mischief/profile.go:124:	target := fs.String("target", "", "fault target selector (required)")
cmd/mischief/profile.go:125:	asJSON := fs.Bool("json", false, "emit the finding JSON on stdout and nothing else")
cmd/mischief/profile.go:126:	seed := fs.Int64("seed", 0, "seed override (default: the profile's declared seed)")
cmd/mischief/profile.go:127:	dir := fs.String("dir", profileRunDir(), "run dir (the run id becomes a subdirectory)")
cmd/mischief/profile.go:128:	profilesDir := fs.String("profiles-dir", "", "catalog/profiles dir (default: resolved from cwd)")
cmd/mischief/profile.go:129:	catalogDir := fs.String("catalog-dir", "", "catalog/faults dir (default: resolved from cwd)")
cmd/mischief/profile.go:270:	asJSON := fs.Bool("json", false, "emit the compare report as ONE JSON document on stdout")
cmd/mischief/chaos.go:69:	project := fs.String("project", "", "project name (required; the matrix is scoped to THAT project)")
cmd/mischief/chaos.go:70:	tierMax := fs.Int("tier-max", 1, "tier ceiling L0..L5 (default 1: the rootless-bunker workhorse ceiling)")
cmd/mischief/chaos.go:71:	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
cmd/mischief/chaos.go:72:	out := fs.String("out", "", "output dir (default: <project>-chaos under the system temp dir)")
cmd/mischief/chaos.go:73:	asMarkdown := fs.Bool("markdown", false, "print the markdown table to stdout instead of the summary line")
cmd/mischief/chaos.go:155:	lanePath := fs.String("lane", "", "instantiated lane file (required)")
cmd/mischief/chaos.go:156:	laneDir := fs.String("lane-dir", "", "the lane workdir whose .coding-hermes/board is probed (default: the lane file's directory)")
cmd/mischief/chaos.go:209:	project := fs.String("project", "", "owning project (required — the board the row lands on)")
cmd/mischief/chaos.go:210:	board := fs.String("board", "", "board dir to file onto (required; the primary's board, seen through the lane's symlink)")
cmd/mischief/chaos.go:211:	title := fs.String("title", "", "one-line finding (required)")
cmd/mischief/chaos.go:212:	reason := fs.String("reason", "", "deciding evidence (required)")
cmd/mischief/chaos.go:213:	priority := fs.String("priority", "P3", "P0..P3 (default P3)")
cmd/mischief/chaos.go:214:	primitive := fs.String("primitive", "", "the catalog primitive id, when the finding names one")
cmd/mischief/chaos.go:215:	verdict := fs.String("verdict", "", "the graded verdict, when the finding carries one (recovered|degraded|not_recovered|hung|corrupted|no_op|aborted|flaky|void — PRD §6.4, closed)")
cmd/mischief/land.go:31:	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
cmd/mischief/land.go:32:	hold := fs.String("hold", "", "hold id to land (required unless --all)")
cmd/mischief/land.go:33:	all := fs.Bool("all", false, "land every currently-armed hold")
cmd/mischief/main.go:263:	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
cmd/mischief/main.go:264:	asJSON := fs.Bool("json", false, "emit JSON instead of text")
cmd/mischief/main.go:270:	all := fs.Bool("all", false, "include terminal holds (reverted / revert_failed) in the listing")
cmd/mischief/main.go:378:	dir := fs.String("dir", defaultRunDir(), "run dir (journal home)")
cmd/mischief/main.go:379:	all := fs.Bool("all", false, "kill switch: revert every live or stuck hold (AC-6)")
cmd/mischief/main.go:380:	hold := fs.String("hold", "", "revert one hold by id")
cmd/mischief/main.go:385:	retry := fs.Bool("retry", false, "re-execute the recorded inverse of a terminal revert_failed hold (requires --hold; exit 0 only if the retry measures reverted)")
cmd/mischief/main.go:550:	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
cmd/mischief/main.go:551:	selfCheck := fs.Bool("self-check", false, "run the rails self-checks (sanction marker, load gate, scope ladder) and exit non-zero naming what fails")
cmd/mischief/main.go:552:	asJSON := fs.Bool("json", false, "emit the report as JSON (rows with per-primitive missing pieces + the SPEC-11 privileged-surface audit)")
cmd/mischief/main.go:553:	opsPosture := fs.Bool("ops", true, "include the SPEC-11 privileged-surface posture (drop-in/manifest/grants per capability)")
cmd/mischief/main.go:554:	opsPrefix := fs.String("prefix", "", "mischief state prefix for the ops posture (default: the ops package default)")
cmd/mischief/main.go:598:	dir := fs.String("dir", defaultRunDir(), "run dir to own (journal + health.json)")
cmd/mischief/main.go:640:	addr := strings.TrimSpace(os.Getenv(daemon.MetricsAddrEnv))
cmd/mischief/selftest.go:32:	all := fs.Bool("all", false, "selftest every catalog primitive")
cmd/mischief/selftest.go:33:	prim := fs.String("primitive", "", "selftest one primitive by catalog id (e.g. P-001)")
cmd/mischief/selftest.go:34:	dir := fs.String("dir", defaultRunDir(), "run dir (journal home for selftest.jsonl)")
cmd/mischief/selftest.go:35:	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
cmd/mischief/battery.go:56:	quick := fs.Bool("quick", false, "run the parity matrix: the five bunker-qa.sh chaos cells, each replaced by its catalog primitive")
cmd/mischief/battery.go:57:	projects := fs.String("project", "", "owning project(s), comma-separated (REQUIRED for full matrix; use --quick for the parity matrix)")
cmd/mischief/battery.go:58:	prims := fs.String("primitive", "", "primitive id(s), comma-separated (REQUIRED for full matrix; use --quick for the parity matrix)")
cmd/mischief/battery.go:59:	out := fs.String("out", "", "artefact dir (default: the run dir)")
cmd/mischief/battery.go:60:	dir := fs.String("dir", defaultRunDir(), "run dir (journal + artefact home)")
cmd/mischief/battery.go:61:	fileRows := fs.Bool("file-rows", false, "emit findings.jsonl (AC-18 board rows for adverse verdicts)")
cmd/mischief/battery.go:62:	catalogDir := fs.String("catalog-dir", "", "catalog dir (default: catalog/faults from cwd upward)")
cmd/mischief/battery.go:63:	dryRun := fs.Bool("dry-run", false, "print the resolved matrix and exit (nothing executes)")
internal/sanction/sanction_test.go:164:	if v, ok := os.LookupEnv(EnvSanction); ok {
internal/sanction/sanction_test.go:168:	if _, ok := os.LookupEnv(EnvSanctionFile); ok {
internal/sanction/sanction_test.go:268:	if _, had := os.LookupEnv(EnvSanction); had {
internal/sanction/sanction_test.go:272:	if v, _ := os.LookupEnv(EnvSanction); v != EnvSanctionValue {
internal/sanction/sanction.go:98:// (os.LookupEnv, os.Hostname, one marker read). Tests inject the seams —
internal/sanction/sanction.go:101:	// EnvLookup resolves environment variables; nil uses os.LookupEnv.
internal/sanction/sanction.go:142:// InspectEnv gathers the evidence for the real host (os.LookupEnv +
internal/sanction/sanction.go:308:	return os.LookupEnv
internal/backend_signal/backend_signal_test.go:688:	oldPath := os.Getenv("PATH")
internal/backend_docker/live_test.go:250:	_ = os.Getenv
internal/daemon/owner_test.go:25:	if dir := os.Getenv(ownerTestChildEnv); dir != "" {
internal/reverter/owner.go:102:	switch strings.TrimSpace(strings.ToLower(os.Getenv(detachModeOverrideEnv))) {
internal/reverter/owner.go:108:	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" && systemdUserReachable(runtimeDir) {
internal/reverter/owner.go:180:	if p := os.Getenv(spawnProbeEnv); p != "" {
internal/reverter/owner.go:453:		if v := os.Getenv(ownerDirEnv); v != "" {
internal/reverter/owner.go:460:		if v := os.Getenv(ownerHoldIDEnv); v != "" {
internal/reverter/owner.go:467:		if v := os.Getenv(ownerExpiryEnv); v != "" {
internal/reverter/exec.go:78:	if h := os.Getenv("HOME"); h != "" {
internal/reverter/owner_spawn_test.go:261:	if os.Getenv(spawnProbeEnv) != "" {
internal/reverter/owner_spawn_test.go:264:	if _, err := os.Stat(os.Getenv("XDG_RUNTIME_DIR") + "/systemd"); err != nil {
internal/reverter/owner_spawn_test.go:329:	if os.Getenv(spawnProbeEnv) != "/tmp/fake203.sh" {
internal/reverter/live_test.go:139:	if os.Getenv(spawnProbeEnv) != "" {
internal/reverter/live_test.go:142:	if _, err := os.Stat(os.Getenv("XDG_RUNTIME_DIR") + "/systemd"); err != nil {
internal/reverter/live_test.go:225:	if os.Getenv(spawnProbeEnv) != "" {
internal/reverter/live_test.go:228:	if _, err := os.Stat(os.Getenv("XDG_RUNTIME_DIR") + "/systemd"); err != nil {

EXIT: 0

## COMMAND
`python3 tools/check_cli_table.py`

PASS plan (6 flags)
PASS status (3 flags)
PASS revert (4 flags)
PASS arm (9 flags)
PASS land (3 flags)
PASS serve (1 flags)
PASS doctor (5 flags)
PASS battery (8 flags)
PASS chaos matrix (5 flags)
PASS chaos check (2 flags)
PASS chaos finding (7 flags)
PASS sim (7 flags)
PASS profile run (6 flags)
PASS profile list (1 flags)
PASS install (4 flags)
PASS uninstall (5 flags)
PASS audit (3 flags)
PASS retention (5 flags)
PASS plane parse-agent-id (1 flags)
PASS plane record (4 flags)
PASS plane selftest-verdict (1 flags)
PASS plane fold (1 flags)
PASS selftest (4 flags)
WARN version — binary accepts undocumented flag(s): version (flag-gap warning, exit 0: the verb's --help does not list them; discovered by probing with an unknown flag — the table must catch up)
cli-table: PASS — 24 command(s), 0 mismatch(es), 1 warning(s)

EXIT: 0

## COMMAND
`python3 tools/gen-docs.py --check`

OK — 0 stale twin(s)

EXIT: 0

## COMMAND
`python3 tools/check_row_drift.py`

row-drift: scanned 2 prose file(s), resolved 29 row reference(s) against 266 board rows
row-drift: PASS — no closed row cited as current behaviour

EXIT: 0

## COMMAND
`make docs-check`

toolchain: go1.26.9 from the module cache at /mnt/ssd/caches/hermes-go-mod/golang.org/toolchain@v0.0.1-go1.26.9.linux-amd64/bin/go (satisfies go.mod's toolchain go1.26.9; downloaded under GOMODCACHE=/mnt/ssd/caches/hermes-go-mod)
bin: bin/mischief (mischief 456c6d437499, toolchain go1.26.9)
python3 tools/check_row_drift.py
row-drift: scanned 2 prose file(s), resolved 29 row reference(s) against 266 board rows
row-drift: PASS — no closed row cited as current behaviour
python3 tools/gen-docs.py --check
OK — 0 stale twin(s)
python3 tools/check_cli_table.py
PASS plan (6 flags)
PASS status (3 flags)
PASS revert (4 flags)
PASS arm (9 flags)
PASS land (3 flags)
PASS serve (1 flags)
PASS doctor (5 flags)
PASS battery (8 flags)
PASS chaos matrix (5 flags)
PASS chaos check (2 flags)
PASS chaos finding (7 flags)
PASS sim (7 flags)
PASS profile run (6 flags)
PASS profile list (1 flags)
PASS install (4 flags)
PASS uninstall (5 flags)
PASS audit (3 flags)
PASS retention (5 flags)
PASS plane parse-agent-id (1 flags)
PASS plane record (4 flags)
PASS plane selftest-verdict (1 flags)
PASS plane fold (1 flags)
PASS selftest (4 flags)
WARN version — binary accepts undocumented flag(s): version (flag-gap warning, exit 0: the verb's --help does not list them; discovered by probing with an unknown flag — the table must catch up)
cli-table: PASS — 24 command(s), 0 mismatch(es), 1 warning(s)

EXIT: 0

## COMMAND
`boardctl -C /home/kara/mischief validate`

board: /home/kara/mischief/.coding-hermes/board (topology A)
rows: 266 tasks, 662 events, 1 fixtures, header parsed
key uniformity: 171/266 rows carry only canon keys; drift 95 row(s): acceptance(26), attempt(2), ch_trace(35), close_commit(1), completed_by(1), evidence_run_id(1), fix(2), gitreins_task(1), note(2), op(6), previous_blocked_reason(4), previous_status(4), primitive(3), reason(3), resolution(29), session(5), severity(1), supersedes(5), verdict(8), witness(9) — 2 fixable by 'boardctl update <id> --normalize'
[warn] tasks.jsonl line 1 (task NEVER-DONE): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 3 (task MSF-002): ci_result "no CI configured" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 14 (task MSF-013): ci_result "green (go build+vet+test on merged main a396ef4)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 15 (task MSF-014): key(s) {ch_trace} outside the declared canon
[warn] tasks.jsonl line 16 (task MSF-015): key(s) {ch_trace} outside the declared canon
[warn] tasks.jsonl line 17 (task MSF-016): ci_result "green (go build+vet+test on merged main a396ef4)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 19 (task MSF-018): ci_result "green (go build+vet+test on merged main a396ef4)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 22 (task MSF-020): guard_result "gitreins tier1 green; go build/vet/test -count=1 all ok in worktree" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 22 (task MSF-020): ci_result "pushed 7b29502; CI check pending in report" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 22 (task MSF-020): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 22 (task MSF-020): key(s) {ch_trace, op} outside the declared canon
[warn] tasks.jsonl line 25 (task MSF-023): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 29 (task MSF-026): ci_result "success (gen-docs --check green; run on a071075 + on 7333763)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 38 (task MSF-028): guard_result "gitreins tier1 PASS (608eb716); go build+test 8pkgs ok; live probes a/b/c rerun by foreman" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 38 (task MSF-028): ci_result "gh run success 2026-10-04T11:42:47Z" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 39 (task MSF-029): ci_result "n/a (docs+YAML comments only; no runtime behavior changed)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 41 (task QA-MISCHIEF-001): guard_result "N/A (foreman-direct mechanical scaffold, no guard battery)" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 41 (task QA-MISCHIEF-001): ci_result "GREEN (first-ever run, trouble-agent/mischief, sha c3a2b9e, conclusion success)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 43 (task RELEASE-002): key(s) {ch_trace} outside the declared canon
[warn] tasks.jsonl line 44 (task RELEASE-003): guard_result "goreleaser check ok; make dist snapshot OK; dry-run archive contents verified (binary+.so 0755+LICENSE+README)" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 44 (task RELEASE-003): ci_result "CI run 37652576796 success + Release run 37652576697 success, both on tag sha b123b4e" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 44 (task RELEASE-003): key(s) {ch_trace, op} outside the declared canon
[warn] tasks.jsonl line 45 (task INT-CI-2): guard_result "go test ./internal/backend_docker/ -run TestLiveOOM -count=1 ok 5.022s local; CI green remote" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 45 (task INT-CI-2): key(s) {ch_trace} outside the declared canon
[warn] tasks.jsonl line 47 (task MSF-032): guard_result "PASS (tier-1+tier-2 a15ec85d)" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 47 (task MSF-032): ci_result "CI red (pre-existing INT-CI-2 flake) — MSF-032 fix does not cause regression" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 48 (task INT-CI-3): guard_result "go build/vet clean; go test ./internal/backend_docker/ ./internal/selftest/ -count=1 ok; gitreins tier-1 verdict 5a0b55bd PASS" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 48 (task INT-CI-3): ci_result "GREEN (run 37606103036, sha 961b357)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 48 (task INT-CI-3): key(s) {ch_trace} outside the declared canon
[warn] tasks.jsonl line 49 (task INT-CI-4): guard_result "goreleaser check ok (config-only)" is free-form — not in the guard_result vocabulary {PASS,FAIL,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 49 (task INT-CI-4): ci_result "Release run 37652576697 SUCCESS on tag sha b123b4e (gates: build/vet/test/selftest/battery/shim-smoke all green); 1st consecutive green tag push" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 49 (task INT-CI-4): key(s) {ch_trace, op} outside the declared canon
[warn] tasks.jsonl line 50 (task INT-CI-5): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 51 (task MSF-033): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 52 (task MSF-034): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 53 (task MSF-035): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 54 (task RELEASE-004): key(s) {witness} outside the declared canon
[warn] tasks.jsonl line 55 (task SEC-001): ci_result "GREEN (e99dabe)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 55 (task SEC-001): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 56 (task README-3): key(s) {ch_trace, fix, resolution} outside the declared canon
[warn] tasks.jsonl line 57 (task README-4): key(s) {ch_trace, fix, resolution, severity} outside the declared canon
[warn] tasks.jsonl line 58 (task README-5): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 59 (task README-6): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 60 (task DOC-6): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 61 (task DOC-7): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 62 (task DOC-8): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 63 (task DOC-9): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 64 (task DOC-10): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 65 (task DF-01): key(s) {ch_trace, resolution, supersedes} outside the declared canon
[warn] tasks.jsonl line 67 (task DF-03): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 68 (task DF-04): key(s) {ch_trace, resolution} outside the declared canon
[warn] tasks.jsonl line 70 (task DF-06): key(s) {completed_by, verdict} outside the declared canon
[warn] tasks.jsonl line 71 (task DF-07): key(s) {verdict} outside the declared canon
[warn] tasks.jsonl line 73 (task RELEASE-005): key(s) {ch_trace, witness} outside the declared canon
[warn] tasks.jsonl line 74 (task QA-MISCHIEF-002): ci_result "RED (00:50 run, missing setsid fallback) -> fixed by QA-MISCHIEF-002-FIXUP" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 75 (task QA-MISCHIEF-003): ci_result "n/a - origin is S3 (s3://kara-reports), no GitHub CI for this repo" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 77 (task MISCHIEF-PLANE-001): ci_result "GREEN (29a0f00 run)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 77 (task MISCHIEF-PLANE-001): key(s) {supersedes} outside the declared canon
[warn] tasks.jsonl line 78 (task MISCHIEF-PLANE-002): key(s) {supersedes} outside the declared canon
[warn] tasks.jsonl line 79 (task MISCHIEF-CLI-001): ci_result "GREEN (cd971be run)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 79 (task MISCHIEF-CLI-001): key(s) {supersedes} outside the declared canon
[warn] tasks.jsonl line 80 (task RELEASE-006): title carries "P1" but priority is "P2" — the priority field wins; rewrite the title with 'boardctl update RELEASE-006 --title', or fix the field with 'boardctl update RELEASE-006 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-006 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 80 (task RELEASE-006): key(s) {ch_trace, witness} outside the declared canon
[warn] tasks.jsonl line 81 (task DF-09): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 81 (task DF-09): key(s) {primitive, reason, verdict} outside the declared canon
[warn] tasks.jsonl line 82 (task DF-10): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 82 (task DF-10): key(s) {primitive, reason, verdict} outside the declared canon
[warn] tasks.jsonl line 83 (task DF-11): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 83 (task DF-11): key(s) {primitive, reason, verdict} outside the declared canon
[warn] tasks.jsonl line 84 (task MISCHIEF-CLI-002): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 85 (task MISCHIEF-PLANE-003): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 86 (task MISCHIEF-CHAOS-001): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 87 (task RELEASE-007): title carries "P1" but priority is "P2" — the priority field wins; rewrite the title with 'boardctl update RELEASE-007 --title', or fix the field with 'boardctl update RELEASE-007 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-007 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 87 (task RELEASE-007): key(s) {ch_trace, witness} outside the declared canon
[warn] tasks.jsonl line 88 (task RELEASE-008): title carries "P1" but priority is "P2" — the priority field wins; rewrite the title with 'boardctl update RELEASE-008 --title', or fix the field with 'boardctl update RELEASE-008 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-008 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 88 (task RELEASE-008): key(s) {ch_trace, witness} outside the declared canon
[warn] tasks.jsonl line 89 (task CHAOS-2): key(s) {witness} outside the declared canon
[warn] tasks.jsonl line 90 (task MISCHIEF-DF-12): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 91 (task MISCHIEF-DF-13): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 91 (task MISCHIEF-DF-13): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 93 (task MISCHIEF-DF-15): key(s) {verdict} outside the declared canon
[warn] tasks.jsonl line 94 (task MISCHIEF-DF-16): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 95 (task MISCHIEF-DF-17): key(s) {verdict} outside the declared canon
[warn] tasks.jsonl line 96 (task MISCHIEF-DF-18): ci_result "GREEN (c152319 run)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 97 (task MISCHIEF-DF-19): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 98 (task RELEASE-009): title carries "P1" but priority is "P2" — the priority field wins; rewrite the title with 'boardctl update RELEASE-009 --title', or fix the field with 'boardctl update RELEASE-009 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-009 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 98 (task RELEASE-009): key(s) {ch_trace, supersedes, witness} outside the declared canon
[warn] tasks.jsonl line 99 (task MISCHIEF-DF-20): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 100 (task MISCHIEF-DF-21): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 101 (task MISCHIEF-DF-22): ci_result "GREEN (run on merged HEAD 34aae2a)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 102 (task MISCHIEF-DF-23): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 103 (task MISCHIEF-DF-24): key(s) {close_commit} outside the declared canon
[warn] tasks.jsonl line 104 (task INT-CI-6): ci_result "GREEN (run on merged HEAD 34aae2a)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 105 (task MISCHIEF-DF-25): ci_result "GREEN (1cbfc0e run)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 106 (task MISCHIEF-DF-26): key(s) {verdict} outside the declared canon
[warn] tasks.jsonl line 107 (task QA-MISCHIEF-002-FIXUP): ci_result "GREEN (434677d run)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 108 (task INT-CI-6-FIXUP): ci_result "GREEN (8ac0c0d run)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 108 (task INT-CI-6-FIXUP): key(s) {ch_trace, witness} outside the declared canon
[warn] tasks.jsonl line 111 (task MISCHIEF-DF-27): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 112 (task MISCHIEF-DF-28): key(s) {resolution} outside the declared canon
[warn] tasks.jsonl line 113 (task MISCHIEF-DF-29): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 113 (task MISCHIEF-DF-29): key(s) {op} outside the declared canon
[warn] tasks.jsonl line 114 (task MISCHIEF-DF-30): key(s) {ch_trace} outside the declared canon
[warn] tasks.jsonl line 116 (task MISCHIEF-DF-32): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 117 (task MISCHIEF-DF-33): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 120 (task MISCHIEF-DF-36): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 121 (task MISCHIEF-DF-37): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 122 (task MISCHIEF-DF-38): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 123 (task RELEASE-010): status "closed" is a read alias for "complete" — canonicalize with 'boardctl update RELEASE-010 --normalize'
[warn] tasks.jsonl line 123 (task RELEASE-010): title carries "P1" but priority is "P2" — the priority field wins; rewrite the title with 'boardctl update RELEASE-010 --title', or fix the field with 'boardctl update RELEASE-010 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-010 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 123 (task RELEASE-010): key(s) {acceptance, ch_trace, previous_blocked_reason, previous_status, witness} outside the declared canon
[warn] tasks.jsonl line 124 (task README-7): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 127 (task MISCHIEF-DF-39): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 127 (task MISCHIEF-DF-39): key(s) {op} outside the declared canon
[warn] tasks.jsonl line 128 (task MISCHIEF-CI-1): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 128 (task MISCHIEF-CI-1): key(s) {op} outside the declared canon
[warn] tasks.jsonl line 134 (task MISCHIEF-DF-29-R2): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 138 (task MISCHIEF-DF-46): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 143 (task MISCHIEF-DF-50): ci_result "GREEN (run on 6d4b9a8)" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 148 (task MISCHIEF-CI-2): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 149 (task MISCHIEF-RV-1): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 150 (task MISCHIEF-RV-2): ci_result "PASS" is free-form — not in the ci_result vocabulary {GREEN,RED,SKIP} (valid: that vocabulary or the structured form {"status":"pass|fail|skip|pending","details":"<prose>"}; writes now enforce this; hand-edit the row or rewrite via boardctl update)
[warn] tasks.jsonl line 156 (task MISCHIEF-DF-56): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 157 (task QA-MISCHIEF-005): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 160 (task MISCHIEF-DF-59): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 162 (task QA-MISCHIEF-002-R2): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 167 (task RELEASE-011): status "closed" is a read alias for "complete" — canonicalize with 'boardctl update RELEASE-011 --normalize'
[warn] tasks.jsonl line 167 (task RELEASE-011): title carries "P1" but priority is "P2" — the priority field wins; rewrite the title with 'boardctl update RELEASE-011 --title', or fix the field with 'boardctl update RELEASE-011 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-011 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 167 (task RELEASE-011): key(s) {acceptance, previous_blocked_reason, previous_status} outside the declared canon
[warn] tasks.jsonl line 170 (task MISCHIEF-DF-66): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 174 (task DOC-12): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 176 (task DOC-14): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 179 (task DOC-17): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 180 (task DOC-18): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 184 (task DOC-22): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 187 (task INT-CI-8): key(s) {acceptance} outside the declared canon — fixable with 'boardctl update INT-CI-8 --normalize'
[warn] tasks.jsonl line 188 (task MISCHIEF-DF-68-R2): key(s) {acceptance} outside the declared canon — fixable with 'boardctl update MISCHIEF-DF-68-R2 --normalize'
[warn] tasks.jsonl line 190 (task QA-MISCHIEF-007): key(s) {acceptance, note, previous_blocked_reason, previous_status} outside the declared canon
[warn] tasks.jsonl line 191 (task QA-MISCHIEF-008): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 192 (task DOC-24-R2): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 193 (task MISCHIEF-DF-68-R2-R2): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 194 (task MISCHIEF-DF-69): key(s) {acceptance, ch_trace} outside the declared canon
[warn] tasks.jsonl line 195 (task RELEASE-012): key(s) {acceptance, previous_blocked_reason, previous_status} outside the declared canon
[warn] tasks.jsonl line 196 (task MISCHIEF-RV-20261027-HEALTH-SHAPE): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 197 (task MISCHIEF-DF-70): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 198 (task MISCHIEF-DF-71): key(s) {acceptance, attempt} outside the declared canon
[warn] tasks.jsonl line 199 (task MISCHIEF-DF-72): key(s) {acceptance} outside the declared canon
[warn] tasks.jsonl line 207 (task RELEASE-013): title carries "P0" but priority is "P1" — the priority field wins; rewrite the title with 'boardctl update RELEASE-013 --title', or fix the field with 'boardctl update RELEASE-013 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-013 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 207 (task RELEASE-013): title carries "P0" but priority is "P1" — the priority field wins; rewrite the title with 'boardctl update RELEASE-013 --title', or fix the field with 'boardctl update RELEASE-013 --priority <Pn>', or re-file the row with 'boardctl create --id RELEASE-013 --force --priority <Pn>' if it should be re-filed
[warn] tasks.jsonl line 208 (task QA-MISCHIEF-CHAOS-1): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 209 (task QA-MISCHIEF-DOCS-1): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 210 (task QA-MISCHIEF-CHAOS-2): status "closed" is a read alias for "complete" — canonicalize with 'boardctl update QA-MISCHIEF-CHAOS-2 --normalize'
[warn] tasks.jsonl line 210 (task QA-MISCHIEF-CHAOS-2): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 211 (task QA-MISCHIEF-CHAOS-3): status "closed" is a read alias for "complete" — canonicalize with 'boardctl update QA-MISCHIEF-CHAOS-3 --normalize'
[warn] tasks.jsonl line 211 (task QA-MISCHIEF-CHAOS-3): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 212 (task PROF-001): key(s) {evidence_run_id, gitreins_task} outside the declared canon
[warn] tasks.jsonl line 214 (task PROF-003): key(s) {attempt} outside the declared canon
[warn] tasks.jsonl line 226 (task QA-MISCHIEF-FOREMAN-1): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 227 (task QA-MISCHIEF-FOREMAN-2): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 228 (task QA-MISCHIEF-FOREMAN-3): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 231 (task QA-MISCHIEF-CHAOS-4): created_at absent — the report parser excludes this row from time-based metrics; hand-edit the row to add a parseable created_at
[warn] tasks.jsonl line 243 (task RELEASE-017): key(s) {note} outside the declared canon
[warn] tasks.jsonl line 246 (task DOC-30): key(s) {session} outside the declared canon
[warn] tasks.jsonl line 247 (task DOC-31): key(s) {session} outside the declared canon
[warn] tasks.jsonl line 248 (task DOC-32): key(s) {session} outside the declared canon
[warn] tasks.jsonl line 251 (task DOC-35): key(s) {session} outside the declared canon
[warn] tasks.jsonl line 253 (task DOC-17-R2): key(s) {session} outside the declared canon
[warn] tasks.jsonl line 254 (task DOC-37): title carries "P5" but priority is "P4" — the priority field wins; rewrite the title with 'boardctl update DOC-37 --title', or fix the field with 'boardctl update DOC-37 --priority <Pn>', or re-file the row with 'boardctl create --id DOC-37 --force --priority <Pn>' if it should be re-filed
[warn] events.jsonl line 58: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 59: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 60: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 64: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 65: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 66: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 67: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 68: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 69: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 70: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 71: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 72: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 73: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 74: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 75: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 76: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 77: event id "README-2" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 92: event id "EVT-INT-CI-2-close" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 93: event id "EVT-INT-CI-3-close" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 94: event id "evt-intci4-close-2026-10-07" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 95: event id "evt-intci5-close-2026-10-07" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 96: event id "evt-msf033-035-close-2026-10-08" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 97: event id "evt-release004-close-2026-10-08" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 119: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 120: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 121: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 122: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 123: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 124: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 125: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 126: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 127: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 128: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 129: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 130: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 131: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 133: event id "EVT-DF-03-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 134: event id "EVT-MSF-034-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 135: event id "EVT-MSF-023-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 136: event id "EVT-DF-04-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 137: event id "EVT-DF-09-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 138: event id "EVT-DF-10-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 139: event id "EVT-DF-11-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 140: event id "EVT-MISCHIEF-DF-12-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 141: event id "EVT-RELEASE-005-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 142: event id "EVT-RELEASE-006-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 143: event id "EVT-RELEASE-007-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 144: event id "EVT-RELEASE-008-close-pm-20261013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 145: event id "EVT-1007" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 146: event id "EVT-1008" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 147: event id "EVT-1009" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 148: event id "EVT-1010" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 149: event id "EVT-1011" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 150: event id "EVT-1012" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 151: event id "EVT-1013" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 152: event id "EVT-1014" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 153: event id "EVT-1015" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 162: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 163: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 164: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 165: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 166: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 167: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 168: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 169: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 170: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 171: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 172: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 173: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 174: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 175: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 176: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 177: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 178: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 179: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 180: event id "EVT-1016" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 181: event id "EVT-1017" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 182: event id "EVT-1018" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 239: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 240: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 241: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 242: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 243: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 244: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 245: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 246: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 297: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 367: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 368: event id "" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 370: event has no id field (legacy shape tolerated)
[warn] events.jsonl line 447: event id "EVT-QA-MISCHIEF-DOCS-1-close-pm-20261103" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 448: event id "EVT-MISCHIEF-DF-59-FIXUP-close-pm-20261103" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 449: event id "EVT-QA-MISCHIEF-007-close-pm-20261103" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 450: event id "EVT-RELEASE-012-close-pm-20261103" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 467: event id "EVT-QA-MISCHIEF-CHAOS-1-close-pm-20261104" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 468: event id "EVT-QA-MISCHIEF-DOCS-1-close-pm-20261104" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 469: event id "EVT-QA-MISCHIEF-CHAOS-2-close-pm-20261104" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 470: event id "EVT-QA-MISCHIEF-CHAOS-3-close-pm-20261104" is not a JSON number (legacy shape tolerated)
[warn] events.jsonl line 471: event id "EVT-PM-AC-REPAIR-20261104" is not a JSON number (legacy shape tolerated)
RESULT: OK (267 warning(s))

EXIT: 0
