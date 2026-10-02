# Small Nextgen design improvements, 2026-10-01

Compared Indigo at `b2619d864df00a66455bd9f5f0c83c1ac13ad73d` with the
local `relay-nextgen` checkout. Retained changes affect three runtime files,
without changing storage, scheduling, public frames, worker counts or dependencies.

- **Pace unsuccessful upstream connections.** Adapt Nextgen's capped exponential
  retry with 80–100% jitter and cancellable waits. Indigo previously retried
  established-connection failures immediately, and its early dial backoff added
  nanoseconds rather than seconds. Initial connections and reconnects after
  cursor advancement remain immediate. Delays grow from 0.8–1 second to 24–30
  seconds without progress. Only failed dials count toward Indigo's existing
  16-failure offline policy. Cursor advancement retains Indigo's existing meaning;
  this does not introduce Nextgen's durable terminal-outcome guarantee. Banned
  relay connections are closed before returning.
- **Release completed references.** Following Nextgen's frame-cache eviction,
  zero flushed persistence jobs and vacated subscriber slots before reslicing.
  This releases obsolete event payloads and disconnected subscriber queues while
  retaining backing-array capacity. It does not bound active queue memory.
- **Own replay resources.** Close each replay file on every return path and
  check cancellation before opening and between records. The initial cursor
  scan and a blocking visitor are still not interrupted by these checks.
- **Avoid a replay wrapper allocation per record.** Reuse one `io.LimitedReader`
  per file, resetting its byte limit for every decoded body. The record boundary
  remains enforced. This offsets the measured cost of deterministic file closing.

These borrow the designs in Nextgen's `internal/ingest/manager.go`,
`internal/firehose/frame_cache.go`, and `internal/postgres/output.go`.
Indigo already serializes live frames once, so Nextgen's shared frame cache and
output-wrapper optimization do not offer the same gain here. Global byte budgets,
queue fairness, transactional durability, and error propagation across repo-state
and output persistence require larger behavioral changes and were deferred.

## Performance evidence

Apple M5 Pro, Go 1.27.1, PostgreSQL 18.6, relay and separate load driver each at
GOMAXPROCS 4. Four sources/workers, 16 consumers, 128 warm identities, 16 open/idle
SQL connections. Each sample uses a fresh process and isolated local database.
No competing agent builds/tests ran during timing. PostgreSQL CPU is not measured.

The final comparison used identical signed fixtures and prebuilt original/final
binaries, alternating before/after/after/before/before/after/after/before/before/after.
Five samples per variant contained 128 warmup creates and 8,192 measured updates
with approximately 5 KiB record payloads. Values below are medians.

| Measurement | Original | Final | Change |
| --- | ---: | ---: | ---: |
| 4,000/s offered: delivered events/s | 3,881.4 | 3,878.4 | -0.08% |
| 4,000/s offered: p99 delivery latency | 103.73 ms | 103.71 ms | -0.02% |
| 4,000/s offered: relay CPU/event | 226.65 µs | 231.52 µs | +2.15% |
| 4,000/s offered: allocated bytes/event | 64,394 | 65,108 | +1.11% |
| Unpaced burst: delivered events/s | 7,910.1 | 7,968.6 | +0.74% |
| Unpaced burst: p99 delivery latency | 1,022.83 ms | 1,017.57 ms | -0.51% |
| Unpaced burst: relay CPU/event | 189.87 µs | 189.20 µs | -0.35% |
| Unpaced burst: allocated bytes/event | 63,554 | 63,632 | +0.12% |

A longer 16,384-update burst used the same final binaries, three samples per
variant in before/after/after/before/before/after order. Delivered throughput was
8,368.6 → 8,395.7 events/s (+0.33%); p99 was 1,936.25 → 1,930.39 ms; CPU/event
178.70 → 181.63 µs (+1.64%); allocation volume 62,747 → 62,757 bytes/event (+0.02%).

Throughput stayed within 1% of baseline and p99 did not regress. CPU ranges
intersect in all cases; small CPU/allocation increases are reported rather than
claiming every resource metric improved. The evidence supports retaining these
small reliability/resource fixes, not an absolute zero-regression guarantee or
production capacity claim. RSS medians varied in both directions; there is no
claimed measured RSS reduction. Stale-reference release has deterministic tests.

All 26 final samples passed exact public-body/CAR preservation, counts, sequence
and per-DID order, and replay checks with four source connections and no replayed
input. The [individual samples](testdata/nextgen-performance.json) preserve metric
values, fixture hashes and runtime source hashes. Raw manifests, fixtures,
source overlays, binaries and logs remain in `/tmp/indigo-port-final` and
`/tmp/indigo-port-long-final`; these temporary paths are not durable storage.

An exploratory 32,768-update unpaced burst overflowed the **original** service's
16,384-entry subscriber queues and failed delivery. It is excluded from comparison,
not counted as a passing capacity sample; logs remain in `/tmp/indigo-port-long`.
An earlier candidate's 0.5% short-burst decline did not repeat in final comparisons.

`BenchmarkPlaybackLog` reads 128 sync records per operation. Five one-second
samples per version at GOMAXPROCS 4, separate from service measurements:

| Record size | Original / final time per 128 records | Original / final allocations |
| --- | ---: | ---: |
| 5 KiB | 178.87 / 178.73 µs | 1,419 / 1,292 |
| 100 KiB | 1,530.08 / 1,471.49 µs | 1,442 / 1,316 |

Closing files/checking cancellation initially cost about 2% on the 5 KiB case;
reader reuse restored baseline speed and removed nearly one allocation per record.
These grouped microbenchmarks are descriptive, not a formal significance claim.
Raw output is in `/tmp/indigo-port-evidence/playback-{before,after,final}.txt`.

## Validation and reproduction

Resource regressions fail against original production files restored with Go
`-overlay`. Retry regressions fail against original retry logic with only the
same injected-dialer test seam. The final relay suite, focused race checks and
vet commands below pass. Review was direct and independently assisted; no Roast
review was run.

```sh
go test ./cmd/relay/... -count=1 -timeout=5m
go test -race ./cmd/relay/relay ./cmd/relay/stream/persist/diskpersist \
  ./cmd/relay/stream/eventmgr -count=1 -timeout=5m
go vet ./cmd/relay/...
go test ./cmd/relay/stream/persist/diskpersist -run '^$' \
  -bench '^BenchmarkPlaybackLog$' -benchmem -benchtime=1s -count=5 -cpu=4
```

The service harness is `relay-nextgen/tests/comparison/run.py`; use
`--implementation indigo --sources 4 --workers 4 --consumers 16 --dids 128
--events 8320 --rate 4000 --repeats 5` and an empty `--output-dir`, repeating with
`--rate 0`. Reuse `--fixture` for byte-identical comparisons. The actual alternating
binary runner and source snapshots are retained at `/tmp/indigo-port-compare.py`
and `/tmp/indigo-port-final/build`. Local PostgreSQL requires loopback sockets and
shared memory; the first sandboxed baseline failed before measurement for that
reason. No production service or configuration was changed; no changes were pushed.
