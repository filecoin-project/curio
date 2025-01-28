---
description: >-
  Curio exposes libp2p protocols so that clients can initiate storage deals with
  the SP
---

# libp2p Protocols

The client makes a deal proposal over `v1.2.0` or `v1.2.1` of the Propose Storage Deal Protocol: - `/fil/storage/mk/1.2.0` or\
\- `/fil/storage/mk/1.2.1`

It is a request / response protocol, where the request and response are CBOR-marshalled.

There are two new fields in the request of `v1.2.1` of the protocol, described in the table below.

### Request

| Field                       | Type               | Description                                                                                        |
| --------------------------- | ------------------ | -------------------------------------------------------------------------------------------------- |
| DealUUID                    | uuid               | A uuid for the deal specified by the client                                                        |
| IsOffline                   | boolean            | Indicates whether the deal is online or offline                                                    |
| ClientDealProposal          | ClientDealProposal | Same as `<v1 proposal>.DealProposal`                                                               |
| DealDataRoot                | cid                | The root cid of the CAR file. Same as `<v1 proposal>.Piece.Root`                                   |
| Transfer.Type               | string             | eg "http"                                                                                          |
| Transfer.ClientID           | string             | Any id the client wants (useful for matching logs between client and server)                       |
| Transfer.Params             | byte array         | Interpreted according to `Type`. eg for "http" `Transfer.Params` contains the http headers as JSON |
| Transfer.Size               | integer            | The size of the data that is sent across the network                                               |
| SkipIPNIAnnounce (v1.2.1)   | boolean            | Whether the provider should announce the deal to IPNI or not (default: false)                      |
| RemoveUnsealedCopy (v1.2.1) | boolean            | Whether the provider should keep an unsealed copy of the deal (default: false)                     |

### Response

| Field    | Type    | Description                                        |
| -------- | ------- | -------------------------------------------------- |
| Accepted | boolean | Indicates whether the deal proposal was accepted   |
| Message  | string  | A message about why the deal proposal was rejected |

## Storage Deal Status Protocol

The client requests the status of a deal over `v1.2.0` of the Storage Deal Status Protocol: `/fil/storage/status/1.2.0`

It is a request / response protocol, where the request and response are CBOR-marshalled.

### Request

| Field     | Type                                                                                                                                      | Description                                        |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| DealUUID  | uuid                                                                                                                                      | The uuid of the deal                               |
| Signature | [Signature](https://github.com/filecoin-project/go-state-types/blob/057cdfb837f7a0309c1607c7c4640f315e51d7af/crypto/signature.go#L36-L39) | A signature over the uuid with the client's wallet |

### Response

| Field               | Type         | Description                                                                                                                                                                                   |
| ------------------- | ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| DealUUID            | uuid         | The uuid of the deal                                                                                                                                                                          |
| Error               | string       | Non-empty if there's an error getting the deal status                                                                                                                                         |
| IsOffline           | boolean      | Indicates whether the deal is online or offline                                                                                                                                               |
| TransferSize        | integer      | The total size of the transfer in bytes                                                                                                                                                       |
| NBytesReceived      | integer      | The number of bytes that have been downloaded                                                                                                                                                 |
| DealStatus.Error    | string       | Non-empty if the deal has failed                                                                                                                                                              |
| DealStatus.Status   | string       | The [checkpoint](https://github.com/filecoin-project/boost/blob/4fb17ba117784479e09db4012a3abf9862b8afd9/storagemarket/types/dealcheckpoints/checkpoints.go#L7-L15) that the deal has reached |
| DealStatus.Proposal | DealProposal |                                                                                                                                                                                               |
| SignedProposalCid   | cid          | cid of the client deal proposal + signature                                                                                                                                                   |
| PublishCid          | cid          | The cid of the publish message, if the deal has been published                                                                                                                                |
| ChainDealID         | integer      | The ID of the deal on chain, if it's been published                                                                                                                                           |
