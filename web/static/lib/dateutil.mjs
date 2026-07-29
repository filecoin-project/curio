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

    return result.trim();
}

export function timeSince(date) {
    const seconds = Math.floor((new Date() - date) / 1000);

    if (seconds < 0) {
        const abs = -seconds;
        if (abs < 60) return 'in a few seconds';
        return `in ${formatDuration(abs)}`;
    }

    const duration = formatDuration(seconds);
    return duration === '' ? 'just now' : duration;
}

export function relativePhrase(date) {
    const rel = timeSince(date);
    if (rel === 'just now' || rel.startsWith('in ')) return rel;
    return `${rel} ago`;
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
