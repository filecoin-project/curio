# SQL Null Dereference Risk Audit

## Critical Issues Found

### 1. **lib/paths/db_index.go:82-87** - CRITICAL NULL DEREFERENCE RISK ⚠️
**Problem**: Accesses `entry.SectorNum.Int64` and `entry.SectorFiletype.Int32` without checking `.Valid` first

```go
// skip sector info for storage paths with no sectors
if !entry.MinerId.Valid {
    continue
}

sectorId := abi.SectorID{
    Miner:  abi.ActorID(entry.MinerId.Int64),  // OK - checked above
    Number: abi.SectorNumber(entry.SectorNum.Int64),  // RISK: SectorNum not checked!
}

byID[id][sectorId] |= storiface.SectorFileType(entry.SectorFiletype.Int32)  // RISK: SectorFiletype not checked!
```

**Root Cause**: LEFT JOIN query can return NULL values for `sector_num` and `sector_filetype` when there's no matching `sector_location` record.

**SQL Query**:
```sql
SELECT stor.storage_id, miner_id, sector_num, sector_filetype, is_primary 
FROM storage_path stor 
LEFT JOIN sector_location sec ON stor.storage_id=sec.storage_id
```

**Fix Required**: Check `SectorNum.Valid` and `SectorFiletype.Valid` before accessing `.Int64`/`.Int32`

**Impact**: Can cause panic if `SectorNum` or `SectorFiletype` are NULL when `MinerId` is valid but comes from a different source

---

## Potential Issues (Need Schema Verification)

### 2. **tasks/indexing/task_indexing.go:238** - POTENTIAL NULL DEREFERENCE RISK
**Problem**: Accesses `task.RawSize` without checking if it can be NULL

```go
pc2, err := commcidv2.PieceCidV2FromV1(pieceCid, uint64(task.RawSize))
```

**Struct Definition**:
```go
type itask struct {
    RawSize  int64 `db:"raw_size"`  // Non-nullable int64
    ...
}
```

**SQL Query**: Uses `p.raw_size` from `market_mk12_deal_pipeline` table

**Schema Check**: From `harmony/harmonydb/sql/20241017-market-mig-indexing.sql`:
```sql
raw_size BIGINT DEFAULT NULL,
```

**Risk**: If `raw_size` is NULL in the database, scanning into `int64` will return zero value, which may cause incorrect behavior.

**Fix Required**: Change `RawSize` to `sql.NullInt64` and check `.Valid` before use, or use COALESCE in SQL query.

---

## Files That Correctly Handle Nullable Types

### ✅ **web/api/webrpc/market.go** - PieceDeal.Offset
- `Offset sql.NullInt64` is properly defined
- Code correctly handles nullable `piece_offset` column

### ✅ **api/types/types.go** - NodeInfo
- Uses `sql.NullString` and `sql.NullTime` correctly
- Properly handles nullable columns from `harmony_machine_details`

### ✅ **lib/paths/db_index.go:940** - isLocked function
- Correctly checks `ts.Valid` before accessing `ts.Time`

---

## Recommendations

1. **For all LEFT JOIN queries**: Ensure all columns from the right table are scanned into nullable types (`sql.Null*`) or use COALESCE to provide defaults
2. **Always check `.Valid`**: Before accessing `.Int64`, `.String`, `.Time`, or `.Bool` on `sql.Null*` types
3. **Schema documentation**: Document which columns can be NULL in the database schema
4. **Add validation**: Consider adding compile-time checks or runtime validation for nullable field access

---

## Files Requiring Immediate Fix

1. **lib/paths/db_index.go:82-87** - CRITICAL FIX NEEDED

---

## Files Requiring Review

1. **tasks/indexing/task_indexing.go:238** - VERIFY NULLABILITY of `raw_size`
2. All files with LEFT JOIN queries - REVIEW FOR NULLABLE COLUMNS
