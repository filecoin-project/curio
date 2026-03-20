UPDATE harmony_task SET name = 'PDPIndexing' WHERE name = 'PDPv0_Indexing';
UPDATE harmony_task SET name = 'PDPIpni' WHERE name = 'PDPv0_IPNI';
UPDATE harmony_task SET name = 'PDPNotify' WHERE name = 'PDPv0_Notify';
UPDATE harmony_task SET name = 'DeleteDataSet' WHERE name = 'PDPv0_DelDataSet';
UPDATE harmony_task SET name = 'PDPInitPP' WHERE name = 'PDPv0_InitPP';
UPDATE harmony_task SET name = 'PDPProvingPeriod' WHERE name = 'PDPv0_ProvPeriod';
UPDATE harmony_task SET name = 'PDPProve' WHERE name = 'PDPv0_Prove';
UPDATE harmony_task SET name = 'TerminateFWSS' WHERE name = 'PDPv0_TermFWSS';

UPDATE harmony_task_history SET name = 'PDPIndexing' WHERE name = 'PDPv0_Indexing';
UPDATE harmony_task_history SET name = 'PDPIpni' WHERE name = 'PDPv0_IPNI';
UPDATE harmony_task_history SET name = 'PDPNotify' WHERE name = 'PDPv0_Notify';
UPDATE harmony_task_history SET name = 'DeleteDataSet' WHERE name = 'PDPv0_DelDataSet';
UPDATE harmony_task_history SET name = 'PDPInitPP' WHERE name = 'PDPv0_InitPP';
UPDATE harmony_task_history SET name = 'PDPProvingPeriod' WHERE name = 'PDPv0_ProvPeriod';
UPDATE harmony_task_history SET name = 'PDPProve' WHERE name = 'PDPv0_Prove';
UPDATE harmony_task_history SET name = 'TerminateFWSS' WHERE name = 'PDPv0_TermFWSS';
