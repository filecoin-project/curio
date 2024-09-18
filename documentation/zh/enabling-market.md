---
description: 如何啟用市場子模組並連接到 Boost
---

# Enabling market
# 啟用市場

## Introduction
## 介紹

Curio 提供了一個市場子模組，可以無縫集成 Boost，而無需對 Boost 代碼庫進行任何更改。本文檔將指導您如何在 Curio 中啟用市場、配置 PiecePark，以及設置現有和新的 Boost 實例。

## Enable Market adapter in Curio
## 在 Curio 中啟用市場適配器

編輯市場配置

```shell
curio config add --title mt01000
```


 添加一個如下的條目： 


  [Subsystems]
  EnableParkPiece = true
  BoostAdapters = ["t10000:127.0.0.1:32100"]


按 `ctrl + D` 保存並退出。

{% hint style="info" %}
每個礦工 ID 應該只運行一個 Curio 市場適配器節點。Boost 一次只能與一個適配器通信。
{% endhint %}

編輯 `/etc/curio.env` 文件，並更新 `CURIO_LAYERS` 變量以包含新的市場層。

``` yaml
CURIO_LAYERS=seal,mt01000
CURIO_ALL_REMAINING_FIELDS_ARE_OPTIONAL=true
CURIO_DB_HOST=yugabyte1,yugabyte2,yugabyte3
CURIO_DB_USER=yugabyte
CURIO_DB_PASSWORD=yugabyte
```


重啟 Curio 服務以使更改生效。

{% hint style="info" %}
運行市場的節點是 Boost 將與之交互的節點。它代理交易數據，因此最好將其運行在 Boost 旁邊或與運行 TreeD（和 TreeRC）任務的同一節點上。TreeD 是我們將交易數據添加到扇區的地方。
{% endhint %}

## Connecting with Boost
## 連接 Boost

### Get Market RPC Info
### 獲取市場 RPC 信息

```shell
curio market rpc-info --layers mt01000
```


{% hint style="info" %}
如果 rpc-info 的輸出為空，那麼您可能沒有包含包含 `BoostAdapters` 配置的正確層。請重新檢查您的 --layers 標誌。
{% endhint %}

### Connect with existing Boost after migration
### 遷移後連接現有的 Boost

使用市場 rpc-info 字符串替換 Boost 配置中的 `SealerApiInfo` 和 `SectorIndexApiInfo` 字符串，然後重啟 Boost。

### Initialising New Boost
### 初始化新的 Boost

按照 [Boost 設置說明](https://boost.filecoin.io/new-boost-setup) 進行操作，並額外更改將 `MINER_API_INFO` 替換為市場 rpc-info 字符串。

### Updating PeerID and On-Chain Address
### 更新 PeerID 和鏈上地址

確保在鏈上設置了您的 SP 的正確 _peer id_ 和 _multiaddr_，因為 `boost init` 會生成一個新的身份。使用以下命令更新鏈上的值：

查找 PeerID

``` shell
boostd net id
```


``` shell
boostd net listen
```


``` bash
sptool --actor <miner id> actor set-addrs <MULTIADDR>
sptool --actor <miner id> actor set-peer-id <PEER_ID>
```
