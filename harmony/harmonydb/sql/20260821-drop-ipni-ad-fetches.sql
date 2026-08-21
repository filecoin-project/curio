-- ipni_ad_fetches recorded raw ad-body fetches, which only proves an indexer
-- pulled the ad, not that it finished processing it. The piece-status
-- endpoint now returns the ad's CID directly so callers can check sync status
-- against the indexer themselves, and "advertised" is tracked in memory by
-- the ipni-provider process (see market/ipni/ipni-provider), so nothing here
-- needs replacing.
DROP TABLE IF EXISTS ipni_ad_fetches;
