-- Changes the data_set column to be nullable in the pdp_data_set_piece_adds table to faciliate create-and-add workflow.

DO $$
BEGIN
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
