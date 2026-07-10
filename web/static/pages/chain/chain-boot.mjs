await Promise.all([
    import('/chain-status.mjs'),
    import('/chain-connectivity.mjs'),
    import('/network-summary.mjs'),
    import('/cluster-machines.mjs'),
    import('/message-queue-summary.mjs'),
    import('/ux/curio-ux.mjs'),
])
