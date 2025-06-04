-- Create a default "public" PDP service for NullAuth operations
-- This ensures all nodes can add data/proofsets without authentication
DO $$
BEGIN
    -- Check if "public" service already exists
    IF NOT EXISTS (SELECT 1 FROM pdp_services WHERE service_label = 'public') THEN
        -- Create a dummy public key for the public service
        -- This is a placeholder key that allows NullAuth operations
        INSERT INTO pdp_services (service_label, pubkey) 
        VALUES ('public', decode('1C0DE4C0FFEE", 'hex'));
        
        -- Log the creation
        RAISE NOTICE 'Created default "public" PDP service for NullAuth operations';
    ELSE
        RAISE NOTICE 'Public PDP service already exists, skipping creation';
    END IF;
END
$$;