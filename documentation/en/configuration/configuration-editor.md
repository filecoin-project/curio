# Configuration editor fidelity

The Curio Configuration editor reflects the current Curio configuration model.
Nested `Dynamic[T]` fields expose `T`, not the internal synchronization wrapper.
Durations are Go duration strings; FIL amounts are strings validated by the
runtime config decoder. The generated source comments supply field help.
The separately built Skiff editor intentionally exposes its smaller model.

A layer is a sparse set of overrides, not a complete effective configuration.
Checking a field includes an override. Zero, false, empty arrays, and values
equal to the public default must remain explicit when saved: they can override
an earlier layer. Unchecking a field removes that override, allowing earlier
layers or defaults to apply. Saving preserves values, not TOML comments or
formatting. Configuration history is not rewritten.

Supported TOML field names are normalized to canonical Go field names for the
editor; literal map keys are not renamed. The existing legacy address-table
and empty-Dynamic-table compatibility path remains in use. Truly unknown keys
stop editing/saving with an error instead of being silently removed. Use a
compatible binary or explicitly review such a layer; do not delete keys just
to dismiss the error. Unrepresentable JavaScript integers also stop the editor
instead of silently rounding a stored value.

The editor checks the loaded layer against the actual schema and verifies that
JSON Editor retained every existing value before enabling Save. It preserves
empty strings in arrays. The save handler independently validates the submitted
and existing layers using the runtime decoder. This is not optimistic locking
between simultaneous human editors, nor a change to runtime policy validation,
dynamic reload, restart requirements, or database schema.

## Regression checks

`TestUIConfigModelCompleteness` walks the actual GET schema and Curio model,
including nested references, structs, pointers, arrays, slices, maps, durations,
FIL, and Dynamic inner types. There are no field exclusions. An unsupported
future type or excluded model field fails and requires explicit review.
`TestUISchemaDocumentation` compares generated help with schema descriptions.

`TestUIGenericLayerConfigRoundTrip` exercises production layer load/save
preparation and runtime loading for subsystem settings, durations, and queue
limits. Additional tests protect explicit default overrides,
unknown keys, case normalization, and nested address/FIL values.

Database-free commands (use the repository's working native build environment,
with database connection variables and integration opt-ins absent):

```sh
go test -tags=cgo,fvm,nosupraseal -count=1 ./web/api/config ./deps/config
go test -race -tags=cgo,fvm,nosupraseal -count=1 ./web/api/config ./deps/config
node --test web/static/config/layer-editor.test.mjs
make cfgdoc-gen
make fiximports
```

These tests do not execute `harmony_config` SQL. Real GUI-instance validation
must separately verify the serving binary, schema response, loaded assets,
and a disposable layer's save/reload behavior. A field rendered in a local
browser fixture does not prove which binary a deployed GUI is serving.
