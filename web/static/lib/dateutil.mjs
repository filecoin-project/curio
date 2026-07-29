function formatDuration(absSeconds) {
    let interval = Math.floor(absSeconds / 86400); // days
    const days = interval;
    interval = absSeconds % 86400;

    let hours = Math.floor(interval / 3600);
    interval = interval % 3600;

    let minutes = Math.floor(interval / 60);

    let result = '';
    if (days > 0) result += `${days}d `;
    if (hours > 0) result += `${hours}h `;
    if (minutes > 0) result += `${minutes}m`;

    return result === '' ? 'just now' : result.trim();
}

export function timeSince(date) {
    const seconds = Math.floor((new Date() - date) / 1000);
    return formatDuration(Math.abs(seconds));
}

function relativePhrase(date) {
    const rel = timeSince(date);
    if (rel === 'just now') return rel;
    const suffix = date.getTime() > Date.now() ? 'from now' : 'ago';
    return `${rel} ${suffix}`;
}

export function formatDate(dateString) {
    const date = new Date(dateString);
    const formattedDate = date.getFullYear() + '-' +
        String(date.getMonth() + 1).padStart(2, '0') + '-' +
        String(date.getDate()).padStart(2, '0') + ' ' +
        String(date.getHours()).padStart(2, '0') + ':' +
        String(date.getMinutes()).padStart(2, '0');

    return `${formattedDate} (${relativePhrase(date)})`;
}

export function formatDateTwo(dateString) {
    const date = new Date(dateString);
    const formattedDate = date.getFullYear() + '-' +
        String(date.getMonth() + 1).padStart(2, '0') + '-' +
        String(date.getDate()).padStart(2, '0') + ' ' +
        String(date.getHours()).padStart(2, '0') + ':' +
        String(date.getMinutes()).padStart(2, '0');

    return [`${formattedDate}`, relativePhrase(date)];
}
