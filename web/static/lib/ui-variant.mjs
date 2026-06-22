import RPCCall from '/lib/jsonrpc.mjs';

let variantPromise;

/** @returns {Promise<'maxboom'|'curio'>} */
export function getUIVariant() {
    if (!variantPromise) {
        variantPromise = RPCCall('UIVariant')
            .then((v) => (v === 'maxboom' ? 'maxboom' : 'curio'))
            .catch(() => 'curio');
    }
    return variantPromise;
}

export async function isMaxBoomUI() {
    return (await getUIVariant()) === 'maxboom';
}

export function applyMaxBoomDocumentClass(variant) {
    if (variant === 'maxboom') {
        document.documentElement.classList.add('maxboom-mode');
    } else {
        document.documentElement.classList.remove('maxboom-mode');
    }
}
