---
description: How to backup and restore your YugabyteDB database for Curio
---

# YugabyteDB Backup

Maintaining regular backups of your YugabyteDB database is critical for disaster recovery and before performing operations like software downgrades. This guide covers essential backup and restore procedures for your Curio cluster's database.

{% hint style="danger" %}
**Always create a backup before running `curio toolbox downgrade`** or performing any major cluster operations. Database schema changes during upgrades may not be reversible without a backup.
{% endhint %}

## Important: Curio uses a DB *and* a schema

Curio connects to YugabyteDB over **YSQL** (Postgres protocol). Two configuration values matter:

- **Database name** (`CURIO_DB_NAME` / `--db-name`) – default is `yugabyte`
- **Schema** inside that database – Curio uses schema **`curio`** by default

So, on a default setup, you typically want to back up the **`yugabyte`** database (and it will include the `curio` schema).

Before running backups, confirm what Curio is configured to use:

```bash
# If you run Curio with env vars
echo "$CURIO_DB_NAME"

# If Curio is installed on PATH, you can inspect flags/defaults
curio --help | grep -E "db-(name|host|port)"
```

If you’re running Curio via systemd or containers, check the env/flags there:

```bash
# systemd (example)
systemctl cat curio | sed -n '1,200p'

# docker compose (example)
docker compose config | sed -n '1,200p'
```

## Prerequisites

- Access to your YugabyteDB cluster
- The `ysql_dump` and `ysqlsh` utilities (included with YugabyteDB installation)
- Sufficient disk space for backup files

## Backup Methods

### Method 1: Using ysql_dump (Recommended)

The `ysql_dump` utility creates a logical backup of your database that can be restored to any YugabyteDB cluster.

#### Full database backup

```bash
ysql_dump -h <yugabyte-host> -p 5433 -U <username> -d <database> -F c -f curio_backup_$(date +%Y%m%d_%H%M%S).dump
```

**Parameters:**
- `-h`: YugabyteDB host address
- `-p`: YSQL port (default: 5433)
- `-U`: Database username
- `-d`: Database name (often `yugabyte` for Curio defaults)
- `-F c`: Custom format (compressed, supports parallel restore)
- `-f`: Output filename

#### Example with typical Curio defaults

```bash
ysql_dump -h 127.0.0.1 -p 5433 -U yugabyte -d yugabyte -F c -f curio_backup_$(date +%Y%m%d_%H%M%S).dump
```

#### Curio schema backup

If you want to back up only Curio’s schema (and its data), not other schemas in the DB:

```bash
ysql_dump -h <yugabyte-host> -p 5433 -U <username> -d <database> --schema=curio -F c -f curio_schema_$(date +%Y%m%d_%H%M%S).dump
```

If you want **schema-only (DDL only)** for Curio (no data):

```bash
ysql_dump -h <yugabyte-host> -p 5433 -U <username> -d <database> --schema=curio --schema-only -f curio_schema_$(date +%Y%m%d_%H%M%S).sql
```

### Method 2: Using ysqlsh with COPY

For smaller exports or specific tables:

```bash
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -d <database> -c "\COPY <table_name> TO 'table_backup.csv' WITH CSV HEADER"
```

## Restore Procedures

### Restore from ysql_dump backup

{% hint style="warning" %}
Ensure all Curio nodes are **stopped** before restoring a backup.
{% endhint %}

#### Full restore (entire database)

```bash
# Drop and recreate the database (ONLY if you intend to restore the full DB)
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -c "DROP DATABASE IF EXISTS <database>;"
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -c "CREATE DATABASE <database>;"

# Restore from backup
pg_restore -h <yugabyte-host> -p 5433 -U <username> -d <database> -F c curio_backup_YYYYMMDD_HHMMSS.dump
```

#### Restore only the Curio schema

If you created a Curio-schema dump (`--schema=curio`), restore into the existing DB:

```bash
pg_restore -h <yugabyte-host> -p 5433 -U <username> -d <database> -F c curio_schema_YYYYMMDD_HHMMSS.dump
```

If you created a Curio schema-only SQL file (`--schema-only`), apply it with ysqlsh:

```bash
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -d <database> -f curio_schema_YYYYMMDD_HHMMSS.sql
```

## Automated Backup Script (example)

```bash
#!/bin/bash
# curio-db-backup.sh

BACKUP_DIR="/path/to/backups"
YB_HOST="127.0.0.1"
YB_PORT="5433"
YB_USER="yugabyte"
YB_DB="yugabyte"     # Curio default DB name
RETENTION_DAYS=7

mkdir -p "$BACKUP_DIR"

BACKUP_FILE="$BACKUP_DIR/curio_backup_$(date +%Y%m%d_%H%M%S).dump"
ysql_dump -h "$YB_HOST" -p "$YB_PORT" -U "$YB_USER" -d "$YB_DB" -F c -f "$BACKUP_FILE"

if [ $? -eq 0 ]; then
  echo "Backup created successfully: $BACKUP_FILE"
  find "$BACKUP_DIR" -name "curio_backup_*.dump" -mtime +$RETENTION_DAYS -delete
else
  echo "Backup failed!"
  exit 1
fi
```

## Best Practices

1. **Regular backups**: schedule automated daily backups for production clusters
2. **Test restores**: periodically verify backups by performing test restores
3. **Off-site storage**: store backup copies in a different location or cloud storage
4. **Pre-upgrade backups**: always create a fresh backup before upgrading or downgrading Curio
5. **Monitor backup size**: ensure adequate storage capacity

## Troubleshooting

### Connection issues

```bash
# Verify YugabyteDB is running
yugabyted status

# Check connectivity
ysqlsh -h <host> -p 5433 -U yugabyte -c "SELECT version();"
```

### Permission errors

Ensure your database user has sufficient privileges:

```sql
GRANT ALL PRIVILEGES ON DATABASE <database> TO <username>;
```

You may also need privileges on the `curio` schema and its objects depending on how your cluster is secured.

## Additional Resources

- [YugabyteDB Backup and Restore Documentation](https://docs.yugabyte.com/preview/manage/backup-restore/)
- [ysql_dump Reference](https://docs.yugabyte.com/preview/admin/ysql-dump/)
- [YugabyteDB Best Practices](https://docs.yugabyte.com/preview/develop/best-practices-ysql/)
