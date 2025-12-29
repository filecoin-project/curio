-- pdpv0-only renames
--

UPDATE harmony_task SET name = 'PDPv0_Indexing' WHERE name = 'PDPIndexing';
UPDATE harmony_task SET name = 'PDPv0_IPNI' WHERE name = 'PDPIpni';
UPDATE harmony_task SET name = 'PDPv0_Notify' WHERE name = 'PDPNotify';
UPDATE harmony_task SET name = 'PDPv0_DelDataSet' WHERE name = 'DeleteDataSet';
UPDATE harmony_task SET name = 'PDPv0_InitPP' WHERE name = 'PDPInitPP';
UPDATE harmony_task SET name = 'PDPv0_ProvPeriod' WHERE name = 'PDPProvingPeriod';
UPDATE harmony_task SET name = 'PDPv0_Prove' WHERE name = 'PDPProve';
UPDATE harmony_task SET name = 'PDPv0_TermFWSS' WHERE name = 'TerminateFWSS';

UPDATE harmony_task_history SET name = 'PDPv0_Indexing' WHERE name = 'PDPIndexing';
UPDATE harmony_task_history SET name = 'PDPv0_IPNI' WHERE name = 'PDPIpni';
UPDATE harmony_task_history SET name = 'PDPv0_Notify' WHERE name = 'PDPNotify';
UPDATE harmony_task_history SET name = 'PDPv0_DelDataSet' WHERE name = 'DeleteDataSet';
UPDATE harmony_task_history SET name = 'PDPv0_InitPP' WHERE name = 'PDPInitPP';
UPDATE harmony_task_history SET name = 'PDPv0_ProvPeriod' WHERE name = 'PDPProvingPeriod';
UPDATE harmony_task_history SET name = 'PDPv0_Prove' WHERE name = 'PDPProve';
UPDATE harmony_task_history SET name = 'PDPv0_TermFWSS' WHERE name = 'TerminateFWSS';