await Promise.all([
    import('/chain-status.mjs'),
    import('/pdp-overview.mjs'),
    import('/ux/curio-ux.mjs'),
])
