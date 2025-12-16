-- pdpv0-only renames
--
-- Rename legacy PDP tables to pdpv0_* names where needed.
--
-- Requirement:
-- - if table exists named `pdp_prove_tasks`
-- - and no table exists named `pdpv0_prove_tasks`
-- - then rename `pdp_prove_tasks` -> `pdpv0_prove_tasks`

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = current_schema()
          AND table_name = 'pdp_prove_tasks'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.tables
        WHERE table_schema = current_schema()
          AND table_name = 'pdpv0_prove_tasks'
    ) THEN
        ALTER TABLE pdp_prove_tasks RENAME TO pdpv0_prove_tasks;
        RAISE NOTICE 'Renamed table pdp_prove_tasks -> pdpv0_prove_tasks';
    ELSE
        RAISE NOTICE 'Skipping rename pdp_prove_tasks -> pdpv0_prove_tasks (source missing or destination already exists)';
    END IF;
END
$$;

UPDATE harmony_task SET name = 'PDPv0_Indexing' WHERE name = 'PDPIndexing';
UPDATE harmony_task SET name = 'PDPv0_IPNI' WHERE name = 'PDPIpni';
UPDATE harmony_task SET name = 'PDPv0_Notify' WHERE name = 'PDPNotify';
UPDATE harmony_task SET name = 'PDPv0_DelDataSet' WHERE name = 'DeleteDataSet';
UPDATE harmony_task SET name = 'PDPv0_InitPP' WHERE name = 'PDPInitPP';
UPDATE harmony_task SET name = 'PDPv0_ProvPeriod' WHERE name = 'PDPProvingPeriod';
UPDATE harmony_task SET name = 'PDPv0_Prove' WHERE name = 'PDPProve';
UPDATE harmony_task SET name = 'PDPv0_TermFWSS' WHERE name = 'TerminateFWSS';