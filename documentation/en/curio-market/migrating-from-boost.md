---
description: A step-by-step Guide to Migrating From Boost to Curio
---

# Migrating From Boost

Migrating from Boost to Curio involves transitioning all deal data, including Boost deals, legacy deals, and Direct Data Onboarding (DDO) deals, to Curio’s storage and indexing system. Below is a detailed guide that walks through each step of the migration process.

## Build the `migrate-curio` CLI Tool

* First, you need to build the `migrate-curio` tool, which is part of the Boost code base. This tool will handle the migration of deals.
*   Run the following command to build the tool.\


    ```bash
    bashCopy codemake migrate-curio
    ```

## Preparation for Migration

* Ensure that both Boost and Curio setups are ready for the migration. **Boost must be shutdown and no deal should be in process**. You will need the following details:
  * Path to the Boost repository (default is `~/.boost`)
  * Backup your Boost repository
  * Database credentials for Curio’s HarmonyDB (host, username, password, port)

## Run the Migration Tool

Use the `migrate-curio` tool to begin the migration process.

```bash
./migrate-curio migrate --boost-repo /path/to/boost-repo \
    --db-host "127.0.0.1" \
    --db-name "yugabyte" \
    --db-user "yugabyte" \
    --db-password "yugabyte" \
    --db-port "5433"
```

This command starts the migration, pulling data from the Boost repository and pushing it into Curio’s database.

## Migration Details

The migration consists of 3 phases:

### Migrate Boost Deals

1. **Retrieve Active and Completed Deals:**
   * The tool will first query Boost’s database (`boost.db`) for active and completed deals.
   * It retrieves all Boost deals, including those that are still active or recently completed.
2. **Filtering Deals:**
   * Some deals may not be eligible for migration. The following deals are **skipped**:
     * Deals where the checkpoint is below the "add piece" stage.
     * Deals with a fatal retry error.
     * Deals with a sector ID of 0 or where the sector is no longer alive.
     * Deals that have already been migrated (tracked in `migrate-curio.db`).
3. **Processing Each Deal:**
   * For each eligible deal:
     * The deal is inserted into the `market_mk12_deals` table in Curio.
     * Additional information about the deal, such as proposal details, client peer ID, and the CID of the published deal, is migrated.
     * If the deal's sector is unsealed, it is added to the indexing and announcement pipeline (`market_mk12_deal_pipeline_migration`).

### Migrate Legacy Deals

1. **Load Legacy Deals:**
   * Legacy deals are stored in Boost’s LevelDB (`LID`). The tool queries the deals and processes each one.
   * It uses the `go-ds-versioning` library to access the old FSM (Finite State Machine) for managing legacy deals.
2. **Skipping Unnecessary Legacy Deals:**
   * Deals that have expired, have invalid sector numbers, or are already migrated are skipped.
   * Deals that do not have a chain deal ID or are past their expiration epoch are excluded from migration.
3. **Processing Legacy Deals:**
   * Legacy deals are inserted into Curio’s database:
     * Signed proposal CID, piece size, start and end epochs, and other deal-specific details are migrated.
   * Deals are not indexed.

### Migrate Direct Data Onboarding (DDO) Deals

1. **Retrieve DDO Deals:**
   * The tool queries the DDO database for all deals created using the Direct Data Onboarding method.
2. **Validating Claims:**
   * For each DDO deal, the tool verifies that the sector matches the claim in the deal. If the sector is no longer active, the deal is skipped.
3. **Processing DDO Deals:**
   * Eligible DDO deals are migrated to Curio’s `market_direct_deals` table.
   * If the sector is unsealed, the deal is also added to the `market_mk12_deal_pipeline_migration` table for indexing and announcement.

### **Deal Announcements**

For all deals that have been migrated and are in unsealed sectors, Curio handles their indexing and IPNI (Indexing Provider Network Interface) announcements, ensuring they are publicly discoverable.

## Cleanup of Local Index Directory (LID) (optional)

*   After the migration, you can clean up the old LevelDB (`LID`) data that was used by Boost:\


    ```bash
    migrate-curio cleanup leveldb --boost-repo <boost-repo-path>
    ```
*   If the `LID` store was using YugabyteDB for storing indexes, use the following command\


    ```bash
    migrate-curio cleanup yugabyte --hosts <yugabyte-hosts> --username <username> --password <password> --connect-string <postgres-connection-string> --i-know-what-i-am-doing

    ./migrate-curio migrate --boost-repo /path/to/boost-repo \
        --db-host "127.0.0.1" \
        --db-name "yugabyte" \
        --db-user "yugabyte" \
        --db-password "yugabyte" \
        --db-port "5433" \
        --connect-string "postgresql://postgres:postgres@localhost" \
        --i-know-what-i-am-doing
    ```

This command will drop the `idx` keyspace and remove relevant tables in YugabyteDB.

## Monitoring and Finalizing Migration

* Once the migration is complete, monitor the logs to ensure all deals have been correctly migrated and indexed.
* Verify that the new Curio storage system has all the Boost, legacy, and DDO deals.
* Verify the Curio is creating new indexing and IPNI jobs for migrated deals in batches.
