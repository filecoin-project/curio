---
description: Curio API references
---

# API

## Groups

* [Allocate](api.md#Allocate)
  * [AllocatePieceToSector](api.md#AllocatePieceToSector)
* [DefaultGroup](api.md#DefaultGroup)
  * [Cordon](api.md#Cordon)
  * [Info](api.md#Info)
  * [Shutdown](api.md#Shutdown)
  * [Uncordon](api.md#Uncordon)
  * [Version](api.md#Version)
* [Log](api.md#Log)
  * [LogList](api.md#LogList)
  * [LogSetLevel](api.md#LogSetLevel)
* [Storage](api.md#Storage)
  * [StorageAddLocal](api.md#StorageAddLocal)
  * [StorageDetachLocal](api.md#StorageDetachLocal)
  * [StorageFindSector](api.md#StorageFindSector)
  * [StorageGenerateVanillaProof](api.md#StorageGenerateVanillaProof)
  * [StorageInfo](api.md#StorageInfo)
  * [StorageInit](api.md#StorageInit)
  * [StorageList](api.md#StorageList)
  * [StorageLocal](api.md#StorageLocal)
  * [StorageRedeclare](api.md#StorageRedeclare)
  * [StorageStat](api.md#StorageStat)
### Allocate


#### AllocatePieceToSector
There are not yet any comments for this method.

Perms: write

Inputs:
```json
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
```

Response:
```json
{
  "Sector": 9,
  "Offset": 1032
}
```

### DefaultGroup


#### Cordon


Perms: admin

Inputs: `null`

Response: `{}`

#### Info


Perms: read

Inputs: `null`

Response:
```json
{
  "ID": 123,
  "CPU": 123,
  "RAM": 9,
  "GPU": 1,
  "HostPort": "string value",
  "LastContact": "0001-01-01T00:00:00Z",
  "Unschedulable": true,
  "Name": "string value",
  "StartupTime": "0001-01-01T00:00:00Z",
  "Tasks": "string value",
  "Layers": "string value",
  "Miners": "string value"
}
```

#### Shutdown


Perms: admin

Inputs: `null`

Response: `{}`

#### Uncordon


Perms: admin

Inputs: `null`

Response: `{}`

#### Version
There are not yet any comments for this method.

Perms: admin

Inputs: `null`

Response:
```json
[
  123
]
```

### Log
The log method group has logging methods


#### LogList
There are not yet any comments for this method.

Perms: read

Inputs: `null`

Response:
```json
[
  "string value"
]
```

#### LogSetLevel


Perms: admin

Inputs:
```json
[
  "string value",
  "string value"
]
```

Response: `{}`

### Storage
The storage method group contains are storage administration method


#### StorageAddLocal


Perms: admin

Inputs:
```json
[
  "string value"
]
```

Response: `{}`

#### StorageDetachLocal


Perms: admin

Inputs:
```json
[
  "string value"
]
```

Response: `{}`

#### StorageFindSector


Perms: admin

Inputs:
```json
[
  {
    "Miner": 1000,
    "Number": 9
  },
  1,
  34359738368,
  true
]
```

Response:
```json
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
```

#### StorageGenerateVanillaProof


Perms: admin

Inputs:
```json
[
  "f01234",
  9
]
```

Response: `"Ynl0ZSBhcnJheQ=="`

#### StorageInfo


Perms: admin

Inputs:
```json
[
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8"
]
```

Response:
```json
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
```

#### StorageInit
There are not yet any comments for this method.

Perms: admin

Inputs:
```json
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
```

Response: `{}`

#### StorageList


Perms: admin

Inputs: `null`

Response:
```json
{
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8": [
    {
      "Miner": 1000,
      "Number": 100,
      "SectorFileType": 2
    }
  ]
}
```

#### StorageLocal


Perms: admin

Inputs: `null`

Response:
```json
{
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8": "/data/path"
}
```

#### StorageRedeclare


Perms: admin

Inputs:
```json
[
  "string value",
  true
]
```

Response: `{}`

#### StorageStat


Perms: admin

Inputs:
```json
[
  "76f1988b-ef30-4d7e-b3ec-9a627f4ba5a8"
]
```

Response:
```json
{
  "Capacity": 9,
  "Available": 9,
  "FSAvailable": 9,
  "Reserved": 9,
  "Max": 9,
  "Used": 9
}
```

