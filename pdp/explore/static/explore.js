(function () {
  const LIMIT = 50
  const PREVIEW_MAX_BYTES = 2 * 1024 * 1024

  const statusEl = document.getElementById('status')
  const tableEl = document.getElementById('pieces-table')
  const bodyEl = document.getElementById('pieces-body')
  const pagerEl = document.getElementById('pager')
  const pageInfoEl = document.getElementById('page-info')
  const prevBtn = document.getElementById('prev-btn')
  const nextBtn = document.getElementById('next-btn')
  const titleEl = document.getElementById('title')

  const pathMatch = window.location.pathname.match(/\/explore\/data-sets\/(\d+)\/?$/)
  const dataSetId = pathMatch ? pathMatch[1] : null
  if (!dataSetId) {
    statusEl.textContent = 'Missing or invalid dataset id in URL'
    statusEl.classList.add('err')
    return
  }
  titleEl.textContent = 'Dataset ' + dataSetId
  document.title = 'Dataset ' + dataSetId + ' — Explorer'

  let offset = 0
  let total = 0
  let expandedId = null
  let objectUrls = []

  function setStatus(msg, isErr) {
    statusEl.textContent = msg || ''
    statusEl.classList.toggle('err', !!isErr)
  }

  function formatBytes(n) {
    n = Number(n || 0)
    if (n === 0) return '0 B'
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
    let v = n
    let i = 0
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024
      i++
    }
    return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`
  }

  function formatDate(iso) {
    if (!iso) return '—'
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return String(iso)
    return d.toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC')
  }

  function revokeObjectUrls() {
    for (const u of objectUrls) URL.revokeObjectURL(u)
    objectUrls = []
  }

  function pieceUrl(cid) {
    return `/piece/${encodeURIComponent(cid)}`
  }

  function canPreview(mime, size) {
    if (!mime || size > PREVIEW_MAX_BYTES) return false
    return (
      mime.startsWith('image/') ||
      mime.startsWith('text/') ||
      mime === 'application/json'
    )
  }

  async function copyText(text, btn) {
    try {
      await navigator.clipboard.writeText(text)
      const prev = btn.textContent
      btn.textContent = 'Copied'
      setTimeout(() => { btn.textContent = prev }, 1200)
    } catch (_) {
      btn.textContent = 'Failed'
    }
  }

  async function loadPieces() {
    setStatus('Loading…', false)
    revokeObjectUrls()
    expandedId = null

    const url = `/explore/data-sets/${encodeURIComponent(dataSetId)}/pieces?limit=${LIMIT}&offset=${offset}`
    let res
    try {
      res = await fetch(url, { credentials: 'same-origin' })
    } catch (e) {
      setStatus(`Failed to load: ${e.message || e}`, true)
      tableEl.hidden = true
      pagerEl.hidden = true
      return
    }

    if (!res.ok) {
      const text = await res.text()
      setStatus(`Failed to load (${res.status}): ${text || res.statusText}`, true)
      tableEl.hidden = true
      pagerEl.hidden = true
      return
    }

    const data = await res.json()
    total = Number(data.total || 0)
    const pieces = data.pieces || []

    bodyEl.replaceChildren()
    if (pieces.length === 0) {
      setStatus(total === 0 ? 'No pieces in this dataset.' : 'No pieces on this page.')
      tableEl.hidden = false
      updatePager()
      return
    }

    setStatus(`${total} piece${total === 1 ? '' : 's'}`)
    tableEl.hidden = false

    for (const p of pieces) {
      const tr = document.createElement('tr')
      tr.className = 'main-row'
      tr.dataset.pieceId = String(p.pieceId)
      tr.dataset.pieceCid = p.pieceCid
      tr.dataset.size = String(p.size)

      const tdUp = document.createElement('td')
      tdUp.textContent = formatDate(p.uploadedAt)

      const tdCid = document.createElement('td')
      const wrap = document.createElement('div')
      wrap.className = 'cid-actions'
      const cidSpan = document.createElement('span')
      cidSpan.className = 'cid'
      cidSpan.textContent = p.pieceCid
      const copyBtn = document.createElement('button')
      copyBtn.type = 'button'
      copyBtn.className = 'copy-btn'
      copyBtn.textContent = 'Copy'
      copyBtn.addEventListener('click', (ev) => {
        ev.stopPropagation()
        copyText(p.pieceCid, copyBtn)
      })
      wrap.append(cidSpan, copyBtn)
      tdCid.append(wrap)

      const tdSize = document.createElement('td')
      tdSize.className = 'mono'
      tdSize.textContent = formatBytes(p.size)

      tr.append(tdUp, tdCid, tdSize)
      tr.addEventListener('click', () => toggleExpand(tr, p))
      bodyEl.append(tr)
    }

    updatePager()
  }

  function updatePager() {
    const show = total > LIMIT
    pagerEl.hidden = !show && offset === 0
    if (pagerEl.hidden) return
    pagerEl.hidden = false
    const from = total === 0 ? 0 : offset + 1
    const to = Math.min(offset + LIMIT, total)
    pageInfoEl.textContent = `${from}–${to} of ${total}`
    prevBtn.disabled = offset <= 0
    nextBtn.disabled = offset + LIMIT >= total
  }

  function clearExpandRows() {
    bodyEl.querySelectorAll('tr.expand').forEach((el) => el.remove())
    bodyEl.querySelectorAll('tr.main-row.selected').forEach((el) => el.classList.remove('selected'))
  }

  async function toggleExpand(tr, piece) {
    const id = String(piece.pieceId)
    if (expandedId === id) {
      clearExpandRows()
      revokeObjectUrls()
      expandedId = null
      return
    }

    clearExpandRows()
    revokeObjectUrls()
    expandedId = id
    tr.classList.add('selected')

    const expandTr = document.createElement('tr')
    expandTr.className = 'expand'
    const td = document.createElement('td')
    td.colSpan = 3
    td.innerHTML = '<div class="expand-panel"><p class="note muted">Loading details…</p></div>'
    expandTr.append(td)
    tr.after(expandTr)

    const panel = td.querySelector('.expand-panel')
    const pieceHref = pieceUrl(piece.pieceCid)

    let mime = 'application/octet-stream'
    try {
      const head = await fetch(pieceHref, { method: 'HEAD' })
      mime = head.headers.get('Content-Type') || mime
    } catch (_) {
      /* keep default */
    }

    panel.replaceChildren()

    const dl = document.createElement('dl')
    dl.className = 'expand-kv'
    dl.innerHTML = `
      <dt>MIME</dt><dd class="mono">${escapeHtml(mime)}</dd>
      <dt>Size</dt><dd class="mono">${escapeHtml(formatBytes(piece.size))}</dd>
    `
    panel.append(dl)

    const actions = document.createElement('div')
    actions.className = 'actions'
    const download = document.createElement('a')
    download.href = pieceHref
    download.textContent = 'Download'
    download.setAttribute('download', '')
    actions.append(download)
    panel.append(actions)

    const previewWrap = document.createElement('div')
    previewWrap.className = 'preview'
    panel.append(previewWrap)

    if (!canPreview(mime, Number(piece.size))) {
      const note = document.createElement('p')
      note.className = 'note'
      note.textContent = 'No inline preview for this type or size.'
      previewWrap.append(note)
      return
    }

    try {
      const res = await fetch(pieceHref)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const blob = await res.blob()
      const objUrl = URL.createObjectURL(blob)
      objectUrls.push(objUrl)

      if (mime.startsWith('image/')) {
        const img = document.createElement('img')
        img.src = objUrl
        img.alt = 'Piece preview'
        previewWrap.append(img)
      } else {
        const text = await blob.text()
        const pre = document.createElement('pre')
        pre.textContent = text.slice(0, 200000)
        previewWrap.append(pre)
      }
    } catch (e) {
      const note = document.createElement('p')
      note.className = 'note'
      note.textContent = `Preview failed: ${e.message || e}`
      previewWrap.append(note)
    }
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
  }

  prevBtn.addEventListener('click', () => {
    offset = Math.max(0, offset - LIMIT)
    loadPieces()
  })
  nextBtn.addEventListener('click', () => {
    offset = offset + LIMIT
    loadPieces()
  })

  loadPieces()
})()
