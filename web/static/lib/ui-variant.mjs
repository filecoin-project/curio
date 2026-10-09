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

const REPEAT_VISITOR_KEY = 'RepeatVisitor';

/** First Skiff visit with no PDP wallet goes to the setup guide once. */
export async function maybeRedirectSkiffFirstVisit() {
    if (localStorage.getItem(REPEAT_VISITOR_KEY)) {
        return false;
    }
    if ((await getUIVariant()) !== 'skiff') {
        return false;
    }
    let keyStatus;
    try {
        keyStatus = await RPCCall('PDPKeyStatus');
    } catch {
        return false;
    }
    if (keyStatus?.configured) {
        return false;
    }
    localStorage.setItem(REPEAT_VISITOR_KEY, '1');
    window.location.replace('/pages/pdp-guide/');
    return true;
}
