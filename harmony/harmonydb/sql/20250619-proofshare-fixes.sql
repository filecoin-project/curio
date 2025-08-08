-- if there are multiple entries with the same request_cid, keep the oldest one
DELETE FROM proofshare_queue pq1
WHERE EXISTS (
    SELECT 1 
    FROM proofshare_queue pq2 
    WHERE pq1.request_cid = pq2.request_cid
    AND pq1.obtained_at < pq2.obtained_at
);

create unique index proofshare_queue_request_cid_uindex
 on proofshare_queue (request_cid);