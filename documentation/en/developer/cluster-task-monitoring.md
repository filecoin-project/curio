# Cluster task monitoring

`ClusterTaskSummary` retains its legacy response fields and update-time-based
`SincePosted` meaning. Its additive ownership age is not execution runtime.
The ownership timestamp has explicit claim provenance; timestamps inherited from
older migrations remain unknown. No historical task rows are backfilled.

`ClusterTaskSummaryLimited` provides a bounded snapshot (at most 500 rows),
filtering before selection, running/pending counts, a database observation time,
and nullable ages. The generic view orders each section by ownership/posting age
then task ID, without a task-type priority policy. Pending rows can use whatever
capacity remains after running rows. Controls can reduce that preview to zero.
The row bound is not a bound on database scan cost.

Provider enrichment is batched by task type, and the additive `SpIDs`/`Miners`
arrays preserve all distinct providers. Legacy singular fields are retained;
they are only one representative when a task spans several providers. Failed
enrichment is partial metadata, not failure of the successfully observed tasks.

The view polls five seconds after request settlement, cancels hidden/disconnected
requests, and rejects obsolete responses. A single monotonic display clock may
estimate ages between confirmed snapshots. Pause, failure, and hidden views
freeze those estimates; resume requires a successful fresh response before
interpolation restarts. Server values can decrease on a new observation.

Grouping is consecutive and preserves server order. Controls, retained snapshots,
unknown ages and filter-mismatch notices survive ordinary refreshes. Site-specific
task priorities and shorter preview defaults are not part of this generic policy.

Database-free Go and JavaScript tests exercise response orchestration, null-time
provenance, provider batching, request lifecycle and display clocks. SQL-shape
assertions are not PostgreSQL/Yugabyte execution or browser layout validation.
