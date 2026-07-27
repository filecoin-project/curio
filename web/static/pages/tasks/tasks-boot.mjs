await Promise.all([
    import('/cluster-machines.mjs'),
    import('/cluster-task-history.mjs'),
    import('/harmony-task-counts.mjs'),
    import('/cluster-tasks.mjs'),
    import('/ux/curio-ux.mjs'),
    import('/ux/components/Drawer.mjs'),
]);
