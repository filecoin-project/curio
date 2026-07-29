import { getUIVariant, applySkiffDocumentClass } from '/lib/ui-variant.mjs';

const variant = await getUIVariant();
applySkiffDocumentClass(variant);

await import('./storage-paths-list.mjs');
await import('/ux/curio-ux.mjs');

if (variant !== 'skiff') {
    await import('/storage-use.mjs');
    await import('/storage-gc.mjs');
}
