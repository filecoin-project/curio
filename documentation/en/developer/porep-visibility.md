# Optional PoRep visibility filter

The PoRep sector table initially shows every returned sector, preserving the
existing default. The checkbox can hide sectors that are neither failed nor
past SDR and have no current SDR owner. It affects this view only and is reset
when the page is reconstructed; it does not change scheduling or shared API data.
An embedding page can opt in initially with the `hide-pending-sdr` boolean
attribute. The standard page does not set it; toggling remains reversible.

The API still returns every sector. Page counts distinguish shown, hidden and
returned rows, and existing waiting-stage counters use the full returned list.
They are not claims about sectors omitted by the server's own query limits.

`SDROwned` is additive owner telemetry from the same SQL statement as the sector
snapshot. It is deliberately distinct from execution-start telemetry and from
the older stage-dependent `StartedSDR`. Missing or NULL ownership in an older
response stays visible: absent telemetry is not proof of an unowned sector.
An owner may have claimed a task without entering Do. This filter does not make
an execution-start or task-completion claim, and does not mutate pipeline state.

The filter calculation and production call-site shape have database-free tests.
Actual row-effect and query-cost validation are separate from those tests.
