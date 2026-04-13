-- Changes the data_set column to be nullable in the pdp_data_set_piece_adds table to faciliate create-and-add workflow.
-- Combined migration: make `data_set` nullable and adjust PK
-- YugabyteDB primary key: (add_message_hash HASH, add_message_index ASC)
-- PostgreSQL primary key: (add_message_hash, add_message_index)

DO $$
BEGIN
  -- Step 1: Drop existing PK if it still uses data_set
  IF EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'pdp_data_set_piece_adds_pk'
      AND conrelid = 'pdp_data_set_piece_adds'::regclass
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      DROP CONSTRAINT pdp_data_set_piece_adds_pk;
  END IF;

  -- Step 2: Create the backend-appropriate primary key
  IF POSITION('-YB-' IN version()) > 0 THEN
    EXECUTE 'ALTER TABLE pdp_data_set_piece_adds
      ADD CONSTRAINT pdp_data_set_piece_adds_pk
      PRIMARY KEY (add_message_hash HASH, add_message_index ASC)';
  ELSE
    EXECUTE 'ALTER TABLE pdp_data_set_piece_adds
      ADD CONSTRAINT pdp_data_set_piece_adds_pk
      PRIMARY KEY (add_message_hash, add_message_index)';
  END IF;

  -- Step 3: Make `data_set` nullable if it is currently NOT NULL
  IF EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_name = 'pdp_data_set_piece_adds'
      AND column_name = 'data_set'
      AND is_nullable = 'NO'
  ) THEN
    ALTER TABLE pdp_data_set_piece_adds
      ALTER COLUMN data_set DROP NOT NULL;
  END IF;
END $$;
