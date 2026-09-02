import { maybeRedirectSkiffFirstVisit } from '/lib/ui-variant.mjs'

if (!(await maybeRedirectSkiffFirstVisit())) {
    await Promise.all([
        import('/chain-status.mjs'),
        import('/pdp-overview.mjs'),
        import('/ux/curio-ux.mjs'),
    ])
}
