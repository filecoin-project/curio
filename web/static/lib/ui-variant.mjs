import RPCCall from '/lib/jsonrpc.mjs';

let variantPromise;

/** @returns {Promise<'skiff'|'curio'>} */
export function getUIVariant() {
    if (!variantPromise) {
        variantPromise = RPCCall('UIVariant')
            .then((v) => (v === 'skiff' ? 'skiff' : 'curio'))
            .catch(() => 'curio');
    }
    return variantPromise;
}

export async function isSkiffUI() {
    return (await getUIVariant()) === 'skiff';
}

export function applySkiffDocumentClass(variant) {
    if (variant === 'skiff') {
        document.documentElement.classList.add('skiff-mode');
    } else {
        document.documentElement.classList.remove('skiff-mode');
    }
}
