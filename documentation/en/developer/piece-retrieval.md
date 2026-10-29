---
description: How GET /piece/{cid} finds a piece, first in the open-pieces hash space and then through the legacy deal path.
---

# Piece retrieval

`GET /piece/{cid}` serves a piece CID v2 straight from the open-pieces hash space, with no SQL on a hit. A v1 CID costs one `parked_pieces` lookup to become v2. A miss falls through to the legacy path: market aggregate parents, then the piece's own market deal, unsealed sector, or piece-park read.

```mermaid
flowchart TD
    req["GET /piece/{cid}"] --> deny["denylist + limiter (unchanged)"]
    deny --> cacheChk{"legacy reader cache or error cache hit?"}
    cacheChk -->|"hit (hot deal piece)"| cached["serve the cached reader, or the cached error"]
    cacheChk -->|miss| parse["v2? if v1: parked_pieces piece_cid -> raw size -> v2"]
    parse --> par["run in parallel"]
    par --> hsProbe["probe candidate disks in parallel (in-memory map, stat/HEAD exists)"]
    par --> cql["CQL FindPieceInAggregate(v2)"]
    hsProbe -->|hit| serve["serve from that disk, cancel CQL"]
    hsProbe -->|miss| waitCql["await CQL result"]
    cql --> waitCql
    waitCql -->|"parents found"| parentProbe["same hashspace probe for each parent v2"]
    parentProbe -->|hit| sub["SectionReader(parent, offset, size)"]
    parentProbe -->|miss| gate
    waitCql -->|"not a subpiece"| gate{"retrieval=true and clusterHasDeals says no market_piece_deal rows?"}
    gate -->|"yes, no SQL"| notFound["404 (ErrNoDeal)"]
    gate -->|"no, or retrieval=false"| legacyAgg

    subgraph legacy [Legacy path, last resort: unchanged code, runs under the cache slot]
        legacyAgg{"parents found by CQL?"}
        legacyAgg -->|yes| aggParents["for each parent in CQL order: market deal lookup of the parent (below), SectionReader(parent, offset, size). First readable parent wins"]
        legacyAgg -->|no| ownDeal
        aggParents -->|"a parent was readable"| legacyServe["serve, reader cached 10 min"]
        aggParents -->|"no parent readable"| ownDeal["market deal lookup of the piece itself: v1 size from market_piece_metadata or parked_pieces, then SELECT market_piece_deal by piece_cid and length"]
        ownDeal --> dealRows{"deal rows?"}
        dealRows -->|"none, retrieval=false"| parkedByCid["piece-park by parked_pieces cid and size (indexing before the deal row exists)"]
        dealRows -->|"none, retrieval=true"| noDeal["ErrNoDeal"]
        dealRows -->|"some"| perDeal["try each deal in order"]
        perDeal -->|"MK12, id is not a ULID"| sector["unsealed sector read: sectorReader.ReadPiece"]
        perDeal -->|"MK20 or PDP v1, has piece_ref"| parkRef["piece-park read by piece_ref"]
        sector -->|ok| legacyServe
        parkRef -->|ok| legacyServe
        parkedByCid -->|ok| legacyServe
        sector -->|"fail, try next deal"| perDeal
        parkRef -->|"fail, try next deal"| perDeal
        perDeal -->|"all deals failed"| dealErr["merged error (HTTP 500)"]
        parkedByCid -->|fail| dealErr
    end

    noDeal --> notFound
    dealErr --> errCache["recorded in the 5s error cache, returned to caller"]
    legacyServe --> done["response"]
```

The legacy subgraph is `getPieceReaderFromAggregate`, then `getPieceReaderFromMarketPieceDeal`, with two differences: the parents come from the stage's CQL result instead of a second `FindPieceInAggregate`, and the PDP-specific steps are gone (`getPieceReaderFromPDPPark` and the `pdp_piecerefs` existence branch in the zero-deals case).

The final error keeps `%w` wrapping of the aggregate and deal errors, so `errors.Is(err, ErrNoDeal)` still maps to 404 (for example an aggregate whose parent is not available anywhere), while sector or piece-park read failures stay 500.

PDP v1 pieces reach this path through their `market_piece_deal` rows (`piece_ref` into piece-park), which is why they keep working and why `clusterHasDeals` counts them.
