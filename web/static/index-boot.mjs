import { getUIVariant, applySkiffDocumentClass } from '/lib/ui-variant.mjs'

const variant = await getUIVariant()
const isSkiff = variant === 'skiff'
applySkiffDocumentClass(variant)

const shared = [
    '/chain-status.mjs',
    '/ux/curio-ux.mjs',
]

const skiffModules = [
    '/pdp-overview.mjs',
]

const curioModules = [
    '/cluster-task-history.mjs',
    '/harmony-task-counts.mjs',
    '/cluster-tasks.mjs',
    '/ux/components/Drawer.mjs',
]

for (const mod of shared) {
    await import(mod)
}
if (isSkiff) {
    for (const mod of skiffModules) {
        await import(mod)
    }
} else {
    for (const mod of curioModules) {
        await import(mod)
    }
}

const skiffEl = document.getElementById('skiff-overview')
const curioEl = document.getElementById('curio-overview')
const titleEl = document.getElementById('overview-title')

if (isSkiff) {
    if (skiffEl) skiffEl.hidden = false
    if (titleEl) titleEl.textContent = 'PDP Overview'
    const walletAside = document.getElementById('pdp-wallet-aside')
    if (walletAside) walletAside.hidden = false
} else {
    if (curioEl) curioEl.hidden = false
}
