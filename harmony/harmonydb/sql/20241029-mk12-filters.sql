-- This table allows creating custom pricing rules
CREATE TABLE market_mk12_pricing_filters (
    name TEXT PRIMARY KEY, -- Filter name

    min_duration_days INT NOT NULL DEFAULT 180, -- Minimum Deal Duration in days
    max_duration_days INT NOT NULL DEFAULT 1278, -- Maximum Deal Duration in days

    min_size BIGINT NOT NULL DEFAULT 256, -- Minimum Size of the deal in bytes
    max_size BIGINT NOT NULL DEFAULT 34359738368, -- Maximum Size of the deal in bytes

    price BIGINT NOT NULL DEFAULT 11302806713, -- attoFIL/GiB/Epoch (Default: 1 FIL/TiB/Month)
    verified BOOLEAN NOT NULL DEFAULT FALSE -- If this rules is for verified deals
);

-- This table allows attaching custom pricing rules to specific clients
CREATE TABLE market_mk12_client_filters (
    name TEXT PRIMARY KEY, -- Name of the rule
    active BOOLEAN NOT NULL DEFAULT FALSE, -- If the rules should be applied or not

    wallets TEXT[], -- Array of Client Wallets this rule will apply on
    peer_ids TEXT[], -- Array of Client peerIDs this rule will apply on

    pricing_filters TEXT[], -- Array of pricing filters to apply on this Rule

    max_deals_per_hour BIGINT NOT NULL DEFAULT 0, -- Maximum no of deals accepted per hour
    max_deal_size_per_hour BIGINT NOT NULL DEFAULT 0, -- Cumulative deal size accepted per hour

    additional_info TEXT NOT NULL DEFAULT '' -- Any additional info about the rules to help SP identify the usage
);

-- Table to track allow/deny list for client wallets
CREATE TABLE market_allow_list (
    wallet TEXT PRIMARY KEY,                -- The wallet to allow/deny deals
    status BOOLEAN NOT NULL             -- TRUE for allow, FALSE for deny
);

-- This function is used to remove pricing filter correctly from all client filters in one query
CREATE OR REPLACE FUNCTION remove_pricing_filter(filter_name TEXT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
DECLARE
    filter_count INT;
    updated_filters BIGINT[];
BEGIN
    -- Check if any rows in market_mk12_client_filters are using this filter_name in pricing_filters
    SELECT COUNT(*) INTO filter_count
    FROM market_mk12_client_filters
    WHERE filter_name = ANY(pricing_filters);

    IF filter_count = 0 THEN
            -- No rows are using this filter, proceed to delete the filter from market_mk12_pricing_filters
            DELETE FROM market_mk12_pricing_filters WHERE name = filter_name;
            RETURN;
    END IF;

    -- Loop through each row in market_mk12_client_filters that contains the filter_name
    FOR updated_filters IN
        SELECT array_remove(pricing_filters, filter_name)
        FROM market_mk12_client_filters
        WHERE filter_name = ANY(pricing_filters)
    LOOP
        -- Check if removing the filter_name results in an empty array
        IF array_length(updated_filters, 1) IS NULL OR array_length(updated_filters, 1) = 0 THEN
            RAISE EXCEPTION 'Operation denied: Removing filter % would leave pricing_filters empty for one or more clients.', filter_name;
         END IF;
    END LOOP;

    -- Proceed to update market_mk12_client_filters, removing filter_name from pricing_filters
    UPDATE market_mk12_client_filters
    SET pricing_filters = array_remove(pricing_filters, filter_name)
    WHERE filter_name = ANY(pricing_filters);

    -- Finally, delete the filter from market_mk12_pricing_filters
    DELETE FROM market_mk12_pricing_filters WHERE name = filter_name;
END;
$$;

-- Function to enforce unique entries in the wallets array
CREATE OR REPLACE FUNCTION enforce_unique_wallets()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if the wallets array has any duplicates
    IF (SELECT COUNT(*) FROM unnest(NEW.wallets) AS w GROUP BY w HAVING COUNT(*) > 1) > 0 THEN
        RAISE EXCEPTION 'wallets array contains duplicate values';
END IF;
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Function to enforce unique entries in the peer_ids array
CREATE OR REPLACE FUNCTION enforce_unique_peers()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if the peer_ids array has any duplicates
    IF (SELECT COUNT(*) FROM unnest(NEW.peer_ids) AS p GROUP BY p HAVING COUNT(*) > 1) > 0 THEN
        RAISE EXCEPTION 'peer_ids array contains duplicate values';
END IF;
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Function to enforce unique entries in the pricing_filters array
CREATE OR REPLACE FUNCTION enforce_unique_pricing_filters()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if the pricing_filters array has any duplicates
    IF (SELECT COUNT(*) FROM unnest(NEW.pricing_filters) AS pf GROUP BY pf HAVING COUNT(*) > 1) > 0 THEN
        RAISE EXCEPTION 'pricing_filters array contains duplicate values';
END IF;
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for enforcing uniqueness in wallets array
CREATE TRIGGER unique_wallets_trigger
    BEFORE INSERT OR UPDATE ON market_mk12_client_filters
                         FOR EACH ROW
                         EXECUTE FUNCTION enforce_unique_wallets();

-- Trigger for enforcing uniqueness in peer_ids array
CREATE TRIGGER unique_peers_trigger
    BEFORE INSERT OR UPDATE ON market_mk12_client_filters
                         FOR EACH ROW
                         EXECUTE FUNCTION enforce_unique_peers();

-- Trigger for enforcing uniqueness in pricing_filters array
CREATE TRIGGER unique_pricing_filters_trigger
    BEFORE INSERT OR UPDATE ON market_mk12_client_filters
                         FOR EACH ROW
                         EXECUTE FUNCTION enforce_unique_pricing_filters();

-- Function to enforce naming convention on "name" column
CREATE OR REPLACE FUNCTION enforce_name_naming_convention()
RETURNS TRIGGER AS $$
BEGIN
    -- Check if the "name" column value matches the pattern [A-Za-z0-9_-]
    IF NEW.name !~ '^[A-Za-z0-9_-]+$' THEN
        RAISE EXCEPTION 'Invalid value for name: "%". Only letters, numbers, underscores, and hyphens are allowed.', NEW.name;
END IF;

RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for the market_mk12_client_filters table to enforce the naming convention on "name" column
CREATE TRIGGER enforce_name_convention_market_mk12_client_filters
    BEFORE INSERT OR UPDATE ON market_mk12_client_filters
                         FOR EACH ROW
                         EXECUTE FUNCTION enforce_name_naming_convention();

-- Trigger for the market_mk12_pricing_filters table to enforce the naming convention on "name" column
CREATE TRIGGER enforce_name_convention_market_mk12_pricing_filters
    BEFORE INSERT OR UPDATE ON market_mk12_pricing_filters
                         FOR EACH ROW
                         EXECUTE FUNCTION enforce_name_naming_convention();
