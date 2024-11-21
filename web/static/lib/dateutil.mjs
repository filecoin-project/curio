export function timeSince(date) {
    const now = new Date();
    const seconds = Math.floor((now - date) / 1000);

    let interval = Math.floor(seconds / 86400); // days
    const days = interval;
    interval = seconds % 86400;

    let hours = Math.floor(interval / 3600);
    interval = interval % 3600;

    let minutes = Math.floor(interval / 60);

    let result = '';
    if (days > 0) result += `${days}d `;
    if (hours > 0) result += `${hours}h `;
    if (minutes > 0) result += `${minutes}m`;

    if (result === '') result = 'just now';

    return result.trim();
}

export function formatDate(dateString) {
    const date = new Date(dateString);
    const formattedDate = date.getFullYear() + '-' +
        String(date.getMonth() + 1).padStart(2, '0') + '-' +
        String(date.getDate()).padStart(2, '0') + ' ' +
        String(date.getHours()).padStart(2, '0') + ':' +
        String(date.getMinutes()).padStart(2, '0');

    const timeAgo = timeSince(date);

    return `${formattedDate} (${timeAgo} ago)`;
}