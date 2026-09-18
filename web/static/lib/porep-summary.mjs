// Validate the additive server contract. Missing/partial fields are not zero.
export const sectorCountKeys = ['Total', 'Complete', 'Remaining', 'Failed', 'PostSDR',
    'SDRTotal', 'SDRRunning', 'SDRPreparing', 'SDRWaitingTask', 'SDRWaitingCreate',
    'SDRMissingTask', 'SDROtherTask', 'SDRFailed', 'SDRUnknown'];

export function summaryTotals(rows) {
    if (!Array.isArray(rows)) return null;
    const total = Object.fromEntries(sectorCountKeys.map(k => [k, 0]));
    const actors = new Set();
    let observed = null;
    for (const row of rows) {
        const c = row?.SectorCounts;
        if (!c || typeof row.Actor !== 'string' || actors.has(row.Actor)
            || !Number.isFinite(Date.parse(c.ObservedAt))
            || sectorCountKeys.some(k => !Number.isSafeInteger(c[k]) || c[k] < 0)) return null;
        if (observed !== null && observed !== c.ObservedAt) return null;
        observed = c.ObservedAt;
        actors.add(row.Actor);
        if (c.Total !== c.Complete + c.Remaining
            || c.SDRTotal !== c.SDRRunning + c.SDRPreparing + c.SDRWaitingTask + c.SDRWaitingCreate
                + c.SDRMissingTask + c.SDROtherTask + c.SDRFailed + c.SDRUnknown
            || c.SDRFailed > c.Failed
            || c.Remaining !== c.SDRTotal + c.PostSDR + c.Failed - c.SDRFailed) return null;
        for (const k of sectorCountKeys) {
            total[k] += c[k];
            if (!Number.isSafeInteger(total[k])) return null;
        }
    }
    return total;
}

export function summaryStatus(status, hasData, error = '') {
    if (status === 'ok') return 'Snapshot';
    if (status === 'loading') return hasData ? 'Refreshing — previous snapshot' : 'Loading summary…';
    if (status === 'paused') return hasData ? 'Paused — previous snapshot' : 'Paused — no snapshot';
    return `${hasData ? 'Stale — previous snapshot' : 'Summary unavailable'}${error ? `: ${error}` : ''}`;
}
