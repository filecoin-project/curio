
# Version Compatibility

This is the compatibility matrix for the latest free Curio releases.

| Curio Version       | Lotus Version | Boost       | Yugabyte           | Forest           |
| ------------------- | ------------- | ----------- | ------------------ | ---------------- |
| 1.22.1 / Automatic  | v1.27.X       | Coming soon | 2.20.X / Automatic | 0.19 / Automatic |
| 1.23.0              | v1.28.0-rcX   | Coming soon | 2.20.X / Automatic | 0.19 / Automatic |

No preference is denoted by "X".

Configurations and the number of machines needed:
 A: Lotus, Curio (numerous), Yugabyte (1 or 3), (optional Boost)
 B: Forest, Curio (numerous), Yugabyte (1 or 3)

## Automatic Updates

- Docker has Watchtower which offers automatic updates which work for Yugabyte and Forest.
- Curio can automatically be updated on MainNet through the Debian update process on Ubuntu.
- Today, only Lotus & Boost lacks automatic updates and must be built and deployed.
- Curio's DEBs include curio-cuda (for Nvidia) and curio-opencl (others like ATI).
  - These can be mixed in a Curio cluster as they only relate to the hardware on the box.
  
## Notes

- Forest (0.19+ & Docker Watchtower) is a light alternative to Lotus Client. It meets Curio's needs, but Boost compatability is in development.

## Building for CalibrationNet

- Required for CalibrationNet participation
- Use the Go version specified in curio/GO_VERSION_MIN
- The available Curio branches are named as release/vVERSION like: release/v1.23.4
- CalibrationNet may be a network-version ahead of MainNet.
  - DEBs are only for MainNet releases and will be available early so MainNet upgrades cause no interruption.
