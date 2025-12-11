---
description: How to backup and restore your YugabyteDB database for Curio
---

# YugabyteDB Backup

Maintaining regular backups of your YugabyteDB database is critical for disaster recovery and before performing operations like software downgrades. This guide covers the essential backup and restore procedures for your Curio cluster's database.

{% hint style="danger" %}
**Always create a backup before running `curio toolbox downgrade`** or performing any major cluster operations. Database schema changes during upgrades may not be reversible without a backup.
{% endhint %}

## Prerequisites

- Access to your YugabyteDB cluster
- The `ysql_dump` and `ysqlsh` utilities (included with YugabyteDB installation)
- Sufficient disk space for backup files

## Backup Methods

### Method 1: Using ysql_dump (Recommended)

The `ysql_dump` utility creates a logical backup of your database that can be restored to any YugabyteDB cluster.

#### Full Database Backup

```bash
ysql_dump -h <yugabyte-host> -p 5433 -U <username> -d <database> -F c -f curio_backup_$(date +%Y%m%d_%H%M%S).dump
```

**Parameters:**
- `-h`: YugabyteDB host address
- `-p`: YSQL port (default: 5433)
- `-U`: Database username
- `-d`: Database name (typically `curio` or your configured database name)
- `-F c`: Custom format (compressed, supports parallel restore)
- `-f`: Output filename

#### Example with typical Curio configuration

```bash
ysql_dump -h 127.0.0.1 -p 5433 -U yugabyte -d curio -F c -f curio_backup_$(date +%Y%m%d_%H%M%S).dump
```

#### Schema-Only Backup

To backup only the database schema without data:

```bash
ysql_dump -h <yugabyte-host> -p 5433 -U <username> -d <database> --schema-only -f curio_schema_$(date +%Y%m%d_%H%M%S).sql
```

### Method 2: Using ysqlsh with COPY

For smaller databases or specific tables:

```bash
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -d <database> -c "\COPY <table_name> TO 'table_backup.csv' WITH CSV HEADER"
```

## Restore Procedures

### Restore from ysql_dump backup

#### Full Restore

{% hint style="warning" %}
Ensure all Curio nodes are **stopped** before restoring a backup.
{% endhint %}

```bash
# First, drop and recreate the database if needed
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -c "DROP DATABASE IF EXISTS curio;"
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -c "CREATE DATABASE curio;"

# Restore from backup
pg_restore -h <yugabyte-host> -p 5433 -U <username> -d curio -F c curio_backup_YYYYMMDD_HHMMSS.dump
```

#### Restore from SQL dump

```bash
ysqlsh -h <yugabyte-host> -p 5433 -U <username> -d curio -f curio_backup.sql
```

## Automated Backup Script

Create a backup script for regular automated backups:

```bash
#!/bin/bash
# curio-db-backup.sh

BACKUP_DIR="/path/to/backups"
YB_HOST="127.0.0.1"
YB_PORT="5433"
YB_USER="yugabyte"
YB_DB="curio"
RETENTION_DAYS=7

# Create backup directory if it doesn't exist
mkdir -p "$BACKUP_DIR"

# Create backup
BACKUP_FILE="$BACKUP_DIR/curio_backup_$(date +%Y%m%d_%H%M%S).dump"
ysql_dump -h "$YB_HOST" -p "$YB_PORT" -U "$YB_USER" -d "$YB_DB" -F c -f "$BACKUP_FILE"

if [ $? -eq 0 ]; then
    echo "Backup created successfully: $BACKUP_FILE"
    
    # Remove backups older than retention period
    find "$BACKUP_DIR" -name "curio_backup_*.dump" -mtime +$RETENTION_DAYS -delete
    echo "Cleaned up backups older than $RETENTION_DAYS days"
else
    echo "Backup failed!"
    exit 1
fi
```

Make the script executable and add it to cron:

```bash
chmod +x curio-db-backup.sh

# Add to crontab for daily backups at 2 AM
crontab -e
# Add: 0 2 * * * /path/to/curio-db-backup.sh >> /var/log/curio-backup.log 2>&1
```

## Best Practices

1. **Regular Backups**: Schedule automated daily backups, especially for production clusters
2. **Test Restores**: Periodically verify your backups by performing test restores
3. **Off-site Storage**: Store backup copies in a different location or cloud storage
4. **Pre-upgrade Backups**: Always create a fresh backup before upgrading or downgrading Curio
5. **Monitor Backup Size**: Track backup sizes to ensure adequate storage capacity

## Troubleshooting

### Connection Issues

If you encounter connection errors:

```bash
# Verify YugabyteDB is running
yugabyted status

# Check connectivity
ysqlsh -h <host> -p 5433 -U yugabyte -c "SELECT version();"
```

### Permission Errors

Ensure your database user has sufficient privileges:

```sql
GRANT ALL PRIVILEGES ON DATABASE curio TO <username>;
```

## Additional Resources

- [YugabyteDB Backup and Restore Documentation](https://docs.yugabyte.com/preview/manage/backup-restore/)
- [ysql_dump Reference](https://docs.yugabyte.com/preview/admin/ysql-dump/)
- [YugabyteDB Best Practices](https://docs.yugabyte.com/preview/develop/best-practices-ysql/)
