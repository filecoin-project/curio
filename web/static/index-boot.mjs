import { getUIVariant, applySkiffDocumentClass } from '/lib/ui-variant.mjs';

const variant = await getUIVariant();
const isSkiff = variant === 'skiff';
applySkiffDocumentClass(variant);

if (isSkiff && document.title.includes('Curio')) {
    document.title = document.title.replace(/Curio/g, 'Skiff');
}

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
if (!isSkiff) {
    for (const mod of curioOnly) {
        await import(mod);
    }
}
