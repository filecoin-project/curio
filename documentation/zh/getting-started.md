---
description: 這是一個幫助新用戶熟悉 Curio 的逐步指南
---

# Getting Started
# 入門指南

## Curio Database and Distributed Architecture
## Curio 數據庫和分佈式架構

### Familiarizing Yourself with Curio
### 熟悉 Curio

在深入設置和配置 Curio 之前，我們強烈建議您先熟悉 [Curio 的設計和基本原則](design/)。這些基礎知識將極大地幫助您進行有效的管理和故障排除。

### **HarmonyDB with YugabyteDB**
### **使用 YugabyteDB 的 HarmonyDB**

Curio 利用 YugabyteDB 創建了一個稱為 HarmonyDB 的抽象層。這個 HarmonyDB 主要有兩個用途：

1. **元數據存儲**：它存儲所有與 Curio 相關的元數據。
2. **共識層**：它為 Curio 集群的分佈式架構建立了一個共識層。

{% hint style="danger" %}
我們建議使用至少 3 個節點的 YugabyteDB 集群以實現高可用性和可擴展性。數據庫的丟失將導致 Curio 無法運行。YugabyteDB 也應該定期備份。
{% endhint %}

### Key Features of HarmonyDB
### HarmonyDB 的主要特點

* **高可用性**：確保即使在節點故障的情況下，元數據和共識信息也始終可用。
* **可擴展性**：能夠處理不斷增加的數據量，並隨著 Curio 集群的增長而擴展。
* **一致性**：在 Curio 集群的分佈式節點之間保持數據一致性。

### Benefits of Using YugabyteDB for HarmonyDB
### 使用 YugabyteDB 作為 HarmonyDB 的好處

* **分佈式 SQL**：結合了 SQL 的優點和分佈式數據庫的彈性和可擴展性。
* **容錯能力**：提供強大的容錯能力，確保 Curio 集群的可靠性。
* **多區域部署**：支持跨多個區域部署，以提高性能和冗餘。

## Chain Node
## 鏈節點

Curio 需要訪問至少一個 Filecoin 鏈節點，如 [Lotus](https://lotus.filecoin.io/lotus/get-started/what-is-lotus/) 或 [Forest](https://docs.forest.chainsafe.io/)（正在進行整合）。Curio 使用這個鏈節點來獲取當前的鏈狀態並向鏈發送消息。Curio 支持使用多個鏈節點。

## Network
## 網絡

每個 Curio 節點必須開放以下端口以進行 API 和 GUI 訪問

| 端口  | 詳情                                                                |
| ----- | ------------------------------------------------------------------- |
| 12300 | 默認 API 端口                                                       |
| 4701  | 默認 GUI 端口。並非所有 Curio 節點都需要啟用 GUI                    |
| 32100 | 市場端口。此端口由用戶在配置中啟用 Boost 訪問時確定。               |

## Boost Compatibility
## Boost 兼容性

Boost 與 Curio 完全兼容，可以用於進行交易和檢索數據，就像 `lotus-miner` 一樣。版本兼容性指南可以在 [Boost 文檔](https://boost.filecoin.io/getting-started#boost-and-lotus-compatibility-matrix) 中找到。

## Installing Curio and creating a Curio cluster
## 安裝 Curio 並創建 Curio 集群

了解了 Curio 的內部機制後，您現在可以繼續 [安裝 Curio 二進制文件](installation.md)。我們建議使用 [Debian 包](installation.md#debian-package-installation) 進行安裝，因為它們可以方便地進行安裝、升級和進程管理。安裝完第一個 Curio 二進制文件後，您可以繼續 [設置 Curio](setup.md)，無論您是 [從 lotus-miner 遷移](setup.md#migrating-from-lotus-miner-to-curio) 還是 [初始化新的 minerID](setup.md#initiating-a-new-curio-cluster)。

## Best Practices
## 最佳實踐

我們已經編制了 [一份最佳實踐列表](best-practices.md) 用於部署和維護 Curio 集群。我們鼓勵所有用戶遵循這些建議，以避免潛在的問題。

新用戶還應該熟悉 [Curio 附帶的兩個二進制文件](curio-cli/) 和 [GUI 頁面](curio-gui.md)。
