---
description: Curio API 参考
---

# API

## 组

* [分配](api.md#Allocate)
  * [分配片段到扇区](api.md#AllocatePieceToSector)
* [默认组](api.md#DefaultGroup)
  * [关闭](api.md#Shutdown)
  * [版本](api.md#Version)
* [日志](api.md#Log)
  * [日志列表](api.md#LogList)
  * [设置日志级别](api.md#LogSetLevel)
* [存储](api.md#Storage)
  * [添加本地存储](api.md#StorageAddLocal)
  * [分离本地存储](api.md#StorageDetachLocal)
  * [查找扇区](api.md#StorageFindSector)
  * [存储信息](api.md#StorageInfo)
  * [初始化存储](api.md#StorageInit)
  * [存储列表](api.md#StorageList)
  * [本地存储](api.md#StorageLocal)
  * [存储统计](api.md#StorageStat)
### Allocate
分配

#### AllocatePieceToSector
此方法尚无任何评论。

权限: 写

输入:

[
  "f01234",
  {
    "PublishCid": {
      "/": "bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4"
    },
    "DealID": 5432,
    "DealProposal": {
      "PieceCID": {
        "/": "bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4"
      },
      "PieceSize": 1032,
      "VerifiedDeal": true,
      "Client": "f01234",
      "Provider": "f01234",
      "Label": "",
      "StartEpoch": 10101,
      "EndEpoch": 10101,
      "StoragePricePerEpoch": "0",
      "ProviderCollateral": "0",
      "ClientCollateral": "0"
    },
    "DealSchedule": {
      "StartEpoch": 10101,
      "EndEpoch": 10101
    },
    "PieceActivationManifest": {
      "CID": {
        "/": "bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4"
      },
      "Size": 1032,
      "VerifiedAllocationKey": {
        "Client": 1000,
        "ID": 0
      },
      "Notify": [
        {
          "Address": "f01234",
          "Payload": "Ynl0ZSBhcnJheQ=="
        }
      ]
    },
    "KeepUnsealed": true
  },
  9,
  {
    "Scheme": "string value",
    "Opaque": "string value",
    "User": {},
    "Host": "string value",
    "Path": "string value",
    "RawPath": "string value",
    "OmitHost": true,
    "ForceQuery": true,
    "RawQuery": "string value",
    "Fragment": "string value",
    "RawFragment": "string value"
  },
  {
    "Authorization": [
      "Bearer ey.."
    ]
  }
]

响应:

{
  "Sector": 9,
  "Offset": 1032
}

### DefaultGroup
默认组

#### Shutdown
关闭

权限: 管理员

输入: `null`
输入: `null`

响应: `{}`

#### Version
版本
此方法尚无任何评论。

权限: 管理员

输入: `null`

响应:

[
  123
]

### Log
日志
日志方法组包含日志记录方法

#### LogList
日志列表
此方法尚无任何评论。

权限: 读取

输入: `null`
输入: `null`

响应:

[
  "string value"
]

#### LogSetLevel
设置日志级别

权限: 管理员

输入:

[
  "string value",
  "string value"
]

响应: `{}`

### Storage
存储
存储方法组包含存储管理方法

#### StorageAddLocal
添加本地存储

权限: 管理员

输入:

[
  "string value"
]

响应: `{}`
响应: `{}`

#### StorageDetachLocal
分离本地存储

权限: 管理员

输入:

[
  "string value"
]

响应: `{}`

#### StorageFindSector
查找扇区存储

权限: 管理员

输入:

[
  {
    "Miner": 1000,
    "Number": 9
  },
  1,
  34359738368,
  true
]

响应:

[
  {
    "ID": "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8",
    "URLs": [
      "string value"
    ],
    "BaseURLs": [
      "string value"
    ],
    "Weight": 42,
    "CanSeal": true,
    "CanStore": true,
    "Primary": true,
    "AllowTypes": [
      "string value"
    ],
    "DenyTypes": [
      "string value"
    ],
    "AllowMiners": [
      "string value"
    ],
    "DenyMiners": [
      "string value"
    ]
  }
]

#### StorageInfo
存储信息

权限: 管理员

输入:

[
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8"
]

响应:

{
  "ID": "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8",
  "URLs": [
    "string value"
  ],
  "Weight": 42,
  "MaxStorage": 42,
  "CanSeal": true,
  "CanStore": true,
  "Groups": [
    "string value"
  ],
  "AllowTo": [
    "string value"
  ],
  "AllowTypes": [
    "string value"
  ],
  "DenyTypes": [
    "string value"
  ],
  "AllowMiners": [
    "string value"
  ],
  "DenyMiners": [
    "string value"
  ]
}

#### StorageInit
初始化存储
此方法尚无任何评论。

权限: 管理员

输入:

[
  "string value",
  {
    "ID": "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8",
    "Weight": 42,
    "CanSeal": true,
    "CanStore": true,
    "MaxStorage": 42,
    "Groups": [
      "string value"
    ],
    "AllowTo": [
      "string value"
    ],
    "AllowTypes": [
      "string value"
    ],
    "DenyTypes": [
      "string value"
    ],
    "AllowMiners": [
      "string value"
    ],
    "DenyMiners": [
      "string value"
    ]
  }
]

响应: `{}`

#### StorageList
存储列表

权限: 管理员

输入: `null`

响应:

{
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8": [
    {
      "Miner": 1000,
      "Number": 100,
      "SectorFileType": 2
    }
  ]
}

#### StorageLocal
本地存储

权限: 管理员

输入: `null`

响应:

{
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8": "/data/path"
}

#### StorageStat
存储统计

权限: 管理员

输入:

[
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8"
]

响应:

{
  "Capacity": 9,
  "Available": 9,
  "FSAvailable": 9,
  "Reserved": 9,
  "Max": 9,
  "Used": 9
}
