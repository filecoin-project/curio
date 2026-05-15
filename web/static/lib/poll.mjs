export function pollRPC(fetchFn, intervalMs, { retries = 1, retryMs = 3000 } = {}) {
    async function poll(retriesLeft) {
        try {
            await fetchFn();
        } catch (e) {
            console.warn('RPC poll failed:', e);
            if (retriesLeft > 0) {
                setTimeout(() => poll(retriesLeft - 1), retryMs);
                return;
            }
        }
        setTimeout(() => poll(retries), intervalMs);
    }
    poll(retries);
}
