# Configuration editor

A configuration layer contains explicit overrides, not the complete effective
configuration. Check a field to include its value when saving the layer.
Zero, `false`, empty strings or arrays, and values equal to the default remain
explicit overrides: they can replace values set by an earlier layer.

Uncheck a field to remove its override from this layer. Its value will then
come from an earlier layer or the default.

If an existing value cannot be represented safely, the editor reports an error
and prevents saving rather than silently dropping or changing it. This includes
unsupported fields and integers that JavaScript cannot represent exactly. Use a
compatible editor or review the layer; do not remove values just to dismiss an
error.

Saving preserves configuration values, but not TOML comments or formatting.
