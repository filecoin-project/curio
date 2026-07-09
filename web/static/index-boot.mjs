import { getUIVariant, applySkiffDocumentClass } from '/lib/ui-variant.mjs';

const variant = await getUIVariant();
const isSkiff = variant === 'skiff';
applySkiffDocumentClass(variant);

const shared = [
    './chain-connectivity.mjs',
    './network-summary.mjs',
    './cluster-machines.mjs',
    '/ux/curio-ux.mjs',
];

const curioOnly = [
    './win-stats.mjs',
    './pipeline-porep.mjs',
    './pipeline-stats.mjs',
    './cc-scheduler.mjs',
    './actor-summary.mjs',
];

for (const mod of shared) {
    await import(mod);
}
if (!isSkiff) {
    for (const mod of curioOnly) {
        await import(mod);
    }
}
