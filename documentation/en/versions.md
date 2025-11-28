# Versions

This is the compatibility matrix for the latest free Curio releases.

| Curio Version                                                | Lotus Version | Net     | Boost      | Yugabyte            | Forest           |
|--------------------------------------------------------------|---------------|---------|------------|---------------------|------------------|
| 1.22.1 / Automatic                                           | v1.27.X       | MainNet | v2.3.0-rc2 | 2.20.X / Automatic  | 0.19 / Automatic |
| 1.23.0                                                       | >v1.28.1      | MainNet | v2.3.0     | 2.20.X / Automatic  | 0.19 / Automatic |
| v1.23.1                                                      | >v1.28.1      | MainNet | v2.3.0     | 2.20.x / Automatic  | 0.19 / Automatic |
| <mark style="color:red;background-color:red;">v1.24.0</mark> | v1.30.0-rcX   | MainNet | v2.4.0-rc1 | 2.20.x / Automatic  | 0.21 / Automatic |
| v1.24.1                                                      | v1.30.0-rcX   | MainNet | v2.4.0-rc1 | 2.20.x / Automatic  | 0.21 / Automatic |
| v1.24.2                                                      | v1.30.0       | MainNet | v2.4.0     | 2.20.x / Automatic  | 0.21 / Automatic |
| v1.24.3                                                      | v1.32.0-rcX   | MainNet | v2.4.1     | 2.20.x / Automatic  | 0.21 / Automatic |
| v1.24.4                                                      | v1.32.0-rcX   | MainNet | v2.4.1     | 2.20.x / Automatic  | 0.23 / Automatic |
| v1.24.5                                                      | v1.32.0-rcX   | Mainnet | v2.4.1     | v2024.2 / Automatic | 0.23 / Automatic |
| v1.25.0                                                      | v1.32.2       | Mainnet | NA         | v2024.2 / Automatic | 0.25 / Automatic |
| v1.25.1                                                      | v1.33.0       | Mainnet | NA         | v2024.2 / Automatic | 0.26 / Automatic |
| v1.26.0                                                      | v1.33.1       | Mainnet | NA         | v2024.2 / Automatic | 0.26 / Automatic |
| v1.27.0                                                      | v1.34.0       | Mainnet | NA         | v2025.1 / Automatic | 0.30 / Automatic |
| v1.27.1                                                      | v1.34.1       | Mainnet | NA         | v2025.1 / Automatic | 0.30 / Automatic |
| v1.27.2                                                      | v1.34.1       | Mainnet | NA         | v2025.1 / Automatic | 0.30 / Automatic |

{% hint style="danger" %}
Releases in <mark style="color:red;">red color</mark> are **not recommended**. Please proceed with the next stable release.
{% endhint %}

No preference is denoted by "X".

Configurations and the number of machines needed: A: Lotus, Curio (numerous), YugabyteDB (1 or 3), (optional Boost) B: Forest, Curio (numerous), Yugabyte (1 or 3)

## Automatic Updates

* Docker has Watchtower which offers automatic updates which work for YugabyteDB and Forest.
* Curio can automatically be updated on MainNet through the Debian update process on Ubuntu.
* Today, only Lotus & Boost lacks automatic updates and must be built and deployed.
* Curio's DEBs include curio-cuda (for Nvidia) and curio-opencl (others like ATI).
  * These can be mixed in a Curio cluster as they only relate to the hardware on the box.

## Database Schema Versions
* When the latest Curio starts-up, it applies any upgrades & migrations to Yugabyte's schema.
* This may cause errors on other nodes in your cluster that run the old version (low likelihood), which has the simple solution of completing the upgrade.
* If, however, the upgrade has a serious bug and you need to downgrade, "curio cli downgrade --last_good_date=20250515"

## Notes

* Forest (0.19+ & Docker Watchtower) is a light alternative to Lotus Client. It meets Curio's needs, but Boost compatibility is in development.

## Building for CalibrationNet

* Required for CalibrationNet participation
* Use the Go version specified in curio/GO\_VERSION\_MIN
* The available Curio branches are named as release/vVERSION like: release/v1.23.4
* CalibrationNet may be a network-version ahead of MainNet.
  * DEBs are only for MainNet releases and will be available early so MainNet upgrades cause no interruption.
