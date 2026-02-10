# Versions
# 版本

这是最新免费 Curio 版本的兼容性矩阵。

| Curio 版本         | Lotus 版本   | 网络    | Boost      | Yugabyte           | Forest           |
| ------------------ | ------------ | ------- | ---------- | ------------------ | ---------------- |
| 1.22.1 / 自动      | v1.27.X      | 主网    | v2.3.0-rc2 | 2.20.X / 自动      | 0.19 / 自动      |
| 1.23.0             | >v1.28.1     | 主网    | v2.3.0     | 2.20.X / 自动      | 0.19 / 自动      |

"X"表示无特定偏好。

配置和所需机器数量：A: Lotus、Curio（多个）、YugabyteDB（1或3个）、（可选Boost）B: Forest、Curio（多个）、Yugabyte（1或3个）

## Automatic Updates
## 自动更新

* Docker有Watchtower，可为YugabyteDB和Forest提供自动更新功能。
* Curio可以通过Ubuntu上的Debian更新过程在主网上自动更新。
* 目前，只有Lotus和Boost缺乏自动更新功能，必须手动构建和部署。
* Curio的DEB包包括curio-cuda（用于Nvidia）和curio-opencl（用于其他如ATI）。
  * 这些可以在Curio集群中混合使用，因为它们只与机器上的硬件有关。

## Notes
## 注意事项

* Forest（0.19+和Docker Watchtower）是Lotus客户端的轻量级替代品。它满足Curio的需求，但Boost兼容性仍在开发中。

## Building for CalibrationNet
## 为校准网构建

* 参与校准网需要
* 使用仓库根目录的 `GO_VERSION_MIN` 中指定的 Go 版本
* 可用的Curio分支名称格式为release/vVERSION，如：release/v1.23.4
* 校准网可能比主网领先一个网络版本。
  * DEB包仅用于主网发布，将提前提供，以确保主网升级不会造成中断。
