-- Clear tables in correct order (respecting foreign key constraints)
TRUNCATE TABLE curio.pdp_proof_sets CASCADE;
TRUNCATE TABLE curio.pdp_piecerefs CASCADE;
TRUNCATE TABLE curio.pdp_proofset_root_adds CASCADE;
TRUNCATE TABLE curio.pdp_proofset_roots CASCADE;
TRUNCATE TABLE curio.pdp_prove_tasks CASCADE;
TRUNCATE TABLE curio.pdp_proofset_creates CASCADE;
TRUNCATE TABLE curio.pdp_piece_uploads CASCADE;

