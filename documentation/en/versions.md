
# Version Compatibility

This is the compatibility matrix for the latest free Curio releases.

| Curio Version | Lotus Version | Go Version   | Boost       | Yugabyte |
| ------------- | ------------- | ------------ | ----------- | -------- |
| 1.22.1        | v1.27.X       | 1.22.3 & DEB | Coming soon | 2.20.X   |
| 1.23.0        | v1.28.0-rcX   | 1.22.3       | Coming soon | 2.20.X   |

No preference is denoted by "X".

## Notes

- Different machines should be running Lotus Client, Curio, Yugabyte, and (optionally) Boost.
- Forest 0.19+ is a light alternative to Lotus Client. It meets Curio's needs, but Boost compatability is in development.
- The available branches are named as release/vVERSION like: release/v1.23.4
- DEB: Curio offers pre-built AMD64 Ubuntu packages for nvidia & ATI.
- CalibrationNet may be a network version ahead of MainNet.
  - DEBs are only for MainNet releases and will be available early so MainNet upgrades cause no interruption.
