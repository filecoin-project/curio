import { getUIVariant, applySkiffDocumentClass, maybeRedirectSkiffFirstVisit } from '/lib/ui-variant.mjs'

const variant = await getUIVariant()
const isSkiff = variant === 'skiff'
applySkiffDocumentClass(variant)

// Skiff primary home is PDP Overview; / redirects so brand → / always works.
// First visit with no wallet goes to the PDP guide once.
if (isSkiff) {
    if (!(await maybeRedirectSkiffFirstVisit())) {
        window.location.replace('/pages/pdp-overview/')
    }
} else {
    await Promise.all([
        import('/chain-status.mjs'),
        import('/actor-summary.mjs'),
        import('/porep-overview.mjs'),
        import('/win-stats.mjs'),
        import('/cc-scheduler.mjs'),
        import('/pipeline-porep.mjs'),
        import('/cluster-tasks.mjs'),
        import('/ux/curio-ux.mjs'),
        import('/ux/components/Drawer.mjs'),
    ])
}
