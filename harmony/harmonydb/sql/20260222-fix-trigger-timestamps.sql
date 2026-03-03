/*
  Fix: Trigger functions write TIMESTAMP WITHOUT TIME ZONE into TIMESTAMPTZ columns.

  The trigger functions set_precommit_ready_at(), set_commit_ready_at(), and
  set_update_ready_at() all use `CURRENT_TIMESTAMP AT TIME ZONE 'UTC'` to write
  timestamps. This expression returns TIMESTAMP WITHOUT TIME ZONE (the UTC wall-clock
  time stripped of its timezone marker). When PostgreSQL stores this bare value into a
  TIMESTAMPTZ column, it re-interprets it using the session's timezone setting.

  In non-UTC sessions, this shifts the stored timestamp by the session's UTC offset:
    - UTC-5 (America/New_York): stored 5 hours in the FUTURE
    - UTC+5 (Asia/Karachi): stored 5 hours in the PAST

  The 20260201 fix corrected the *comparison* side (batch functions now use NOW()),
  but left the *write* side unchanged. Before that fix, both sides had the same
  timezone bug so the errors canceled out. After that fix, only the comparison was
  correct, making the effective timeout = configured_timeout + abs(tz_offset).

  Fix: Replace `CURRENT_TIMESTAMP AT TIME ZONE 'UTC'` with `NOW()` in all three
  trigger functions. NOW() returns TIMESTAMPTZ directly, storing correctly regardless
  of the session timezone.
*/

CREATE OR REPLACE FUNCTION set_precommit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.after_tree_r = FALSE AND NEW.after_tree_r = TRUE THEN
        UPDATE sectors_sdr_pipeline SET precommit_ready_at = NOW()
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION set_commit_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.after_porep = FALSE AND NEW.after_porep = TRUE THEN
        UPDATE sectors_sdr_pipeline SET commit_ready_at = NOW()
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION set_update_ready_at()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.after_prove = FALSE AND NEW.after_prove = TRUE THEN
        UPDATE sectors_snap_pipeline SET update_ready_at = NOW()
        WHERE sp_id = NEW.sp_id AND sector_number = NEW.sector_number;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Also fix any existing timestamps that were stored with the wrong timezone.
-- Rows where the ready_at is in the future are clearly affected by the bug.
-- We reset them to NOW() so the timeout starts fresh from this migration.
UPDATE sectors_sdr_pipeline SET precommit_ready_at = NOW()
WHERE precommit_ready_at > NOW() AND after_precommit_msg = FALSE;

UPDATE sectors_sdr_pipeline SET commit_ready_at = NOW()
WHERE commit_ready_at > NOW() AND after_commit_msg = FALSE;

UPDATE sectors_snap_pipeline SET update_ready_at = NOW()
WHERE update_ready_at > NOW() AND after_submit = FALSE;
