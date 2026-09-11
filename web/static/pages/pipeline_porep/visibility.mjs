// Optional presentation filter only. Unknown ownership from an older backend
// stays visible; missing telemetry must not silently hide a sector.
export function visiblePoRepSectors(sectors, hidePendingSDR) {
    if (!hidePendingSDR) return sectors;
    return sectors.filter((sector) => sector.Failed || sector.AfterSDR || sector.SDROwned !== false);
}
