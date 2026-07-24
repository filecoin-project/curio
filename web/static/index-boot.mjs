import { getUIVariant, applySkiffDocumentClass } from '/lib/ui-variant.mjs'

const variant = await getUIVariant()
const isSkiff = variant === 'skiff'
applySkiffDocumentClass(variant)

// Skiff primary home is PDP Overview; keep / as PoRep Overview.
if (isSkiff) {
    window.location.replace('/pages/pdp-overview/')
} else {
    await Promise.all([
        import('/chain-status.mjs'),
        import('/cluster-task-history.mjs'),
        import('/harmony-task-counts.mjs'),
        import('/cluster-tasks.mjs'),
        import('/ux/components/Drawer.mjs'),
        import('/ux/curio-ux.mjs'),
    ])
}
