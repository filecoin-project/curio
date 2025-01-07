---
description: Curio market related UI pages and their content description
---

# Market UI

The "storage market" page is the main page for storage deal market. The page provides quick summary of market balances, piece status, storage ask and deal pipeline status.

<figure><img src="../.gitbook/assets/market_page.png" alt=""><figcaption><p>Storage market UI page</p></figcaption></figure>

You can set the storage ask for each miner ID in the Curio cluster individually from the UI.

<figure><img src="../.gitbook/assets/storage_ask.png" alt="" width="443"><figcaption><p>Storage ask UI</p></figcaption></figure>

By clocking on the UUID column of the deal pipeline in the storage market page, you can find more detailed information about the deal.

<figure><img src="../.gitbook/assets/deal_details.png" alt=""><figcaption><p>Deal detail page</p></figcaption></figure>

The "Storage Deals" page list outs all the details in the Curio market. This can be used to get the summary of latest deals or to lookup a specific deal with unique identifier (UUID) using the search function.

<figure><img src="../.gitbook/assets/deal_list.png" alt=""><figcaption><p>Storage Deals page</p></figcaption></figure>

The "Piece Info" contains all the deals about a piece onboarded by the Curio market. This is a one stop shop for all details about a piece and the deal containing the piece. It provides information about the piece itself, indexing, IPNI announcement status of the piece along with all the deals which contains this piece with their processing status. It also lists out the sectors which contain this piece. This is the single most important page for debugging issues with data onboarding. This page can be opened by clikcing on the "Piece Cid" from multiple pages inclusing the deal detail and deal list pages.

<figure><img src="../.gitbook/assets/piece_info_1.png" alt=""><figcaption><p>Piece Info page</p></figcaption></figure>

<figure><img src="../.gitbook/assets/piece_info3.png" alt=""><figcaption><p>Piece Info deal details</p></figcaption></figure>

<figure><img src="../.gitbook/assets/piece_info2.png" alt=""><figcaption><p>Piece Info storage pipeline status</p></figcaption></figure>

All the IPNI provider and advertisement related details can be found on the "IPNI" page. The current status of IPNI provider (Curio) can be found on this page. The status is listed for each miner ID for each IPNI (ex: cid.contact) node.

<figure><img src="../.gitbook/assets/ipni_provider.png" alt=""><figcaption><p>IPNI page</p></figcaption></figure>

The page also allows searching IPNI advertisements using the Cid. Users can scan the original piece to rebuild the entry list to debug issues with advertisement sync.&#x20;

<figure><img src="../.gitbook/assets/ipni_ad.png" alt=""><figcaption><p>IPNI advertisement search page</p></figcaption></figure>
