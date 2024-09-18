---
description: Curio 中的垃圾回收和清理過程
---

# Garbage Collection
# 垃圾回收

## Sealing Pipeline cleanup
## 密封管道清理

**SDRPipelineGC** 是系統中的一個定期任務，確保存儲密封過程的效率和有效性。它負責清理密封管道中已完成的條目。

### Process
### 過程

GC 通過移除已完成密封過程的密封管道條目來運作。這些條目的元數據已存儲在長期扇區元數據表（也稱為 `sectors_meta` 表）中。此操作有助於保持管道流暢和整潔，提高整體系統性能。

### Handling failed sector
### 處理失敗的扇區

如果一個扇區在密封過程中失敗，其相應的條目可以通過網頁用戶界面（WebUI）手動移除。此功能允許主動管理管道條目，確保失敗的條目不會阻塞管道。

## Storage Cleanup
## 存儲清理

`StorageGCMark` 組件負責掃描系統中的所有扇區文件。

在以下條件下，扇區文件將在 `storage_removal_marks` 表中被標記：

* 該扇區在 `storage_gc_pins` 表中沒有被"固定"。（扇區固定表示即使扇區已過期，也不應被移除。）
* 該扇區不存在於名為 `sectors_sdr_pipeline` 的密封表中。
* 該扇區被標記為"失敗"扇區。（注意，"失敗"的扇區必須首先從管道表中移除，然後才能對其數據進行垃圾回收。）
* 該扇區不存在於礦工參與者預提交扇區集中。
* 該扇區不存在於 `Live` 或 `Unproven` 扇區集中。

### Approval and Removal
### 批准和移除

來自 `StorageGCMark` 過程的移除標記需要單獨批准。目前，此批准僅通過 WebUI 提供。未來可能會擴展以允許自動批准，由自定義的選擇策略支持。

<figure><img src=".gitbook/assets/storage-gc.png" alt=""><figcaption><p>存儲 GC 批准</p></figcaption></figure>

一旦移除標記獲得批准，定期的 `StorageGCSweep` 任務將審查所有已批准的移除標記。然後，此任務將繼續刪除已批准移除的文件。這最後階段確保系統中只保留必要的數據，優化存儲並改善整體系統功能。

### Removing a failed sector
### 移除失敗的扇區

要移除在密封過程中失敗的扇區，用戶應前往 WebUI 的"PoRep"頁面，選擇相應扇區的"DETAILS"鏈接。此操作將引導他們到一個有"Remove"按鈕的頁面。點擊此按鈕後，失敗的扇區將從 SDR 管道表中移除，使其可供 StorageGCMark 過程標記為垃圾回收。

然而，扇區的移除只會在獲得必要的批准後進行。此批准可以在"Storage GC Info"頁面上提供。在收到批准後，StorageGCSweep 任務將審查標記並繼續刪除扇區文件，有效地從系統中移除失敗的扇區。

<figure><img src=".gitbook/assets/remove-sector.png" alt=""><figcaption><p>如何 GC 失敗的扇區</p></figcaption></figure>
