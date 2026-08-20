-- ipni_ad_fetches recorded raw ad-body fetches, which only proves an indexer pulled
-- the ad, not that it finished processing it. Replaced by an in-memory
-- announce/sync-confirmation watermark in market/ipni/ipni-provider.
DROP TABLE IF EXISTS ipni_ad_fetches;
