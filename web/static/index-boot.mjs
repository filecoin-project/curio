import { getUIVariant, applySkiffDocumentClass } from '/lib/ui-variant.mjs'

const variant = await getUIVariant()
const isSkiff = variant === 'skiff'
applySkiffDocumentClass(variant)

// Skiff primary home is PDP Overview; / redirects so brand → / always works.
if (isSkiff) {
    window.location.replace('/pages/pdp-overview/')
} else {
    await Promise.all([
        import('/chain-status.mjs'),
        import('/actor-summary.mjs'),
        import('/porep-overview.mjs'),
        import('/cluster-tasks.mjs'),
        import('/ux/curio-ux.mjs'),
        import('/ux/components/Drawer.mjs'),
    ])
}
