import { getUIVariant, applyMaxBoomDocumentClass } from '/lib/ui-variant.mjs';

const variant = await getUIVariant();
const isMaxBoom = variant === 'maxboom';
applyMaxBoomDocumentClass(variant);

const shared = [
    './chain-connectivity.mjs',
    './network-summary.mjs',
    './harmony-task-counts.mjs',
    './cluster-tasks.mjs',
    './cluster-machines.mjs',
    './cluster-task-history.mjs',
    '/ux/curio-ux.mjs',
    '/ux/components/Drawer.mjs',
];

const curioOnly = [
    './storage-gc.mjs',
    './storage-use.mjs',
    './win-stats.mjs',
    './pipeline-porep.mjs',
    './pipeline-stats.mjs',
    './cc-scheduler.mjs',
    './actor-summary.mjs',
];

for (const mod of shared) {
    await import(mod);
}
if (!isMaxBoom) {
    for (const mod of curioOnly) {
        await import(mod);
    }
}
