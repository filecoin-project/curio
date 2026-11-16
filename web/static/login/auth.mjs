const mode = document.body.dataset.mode || 'login';
const errEl = document.getElementById('err');
const form = document.getElementById('form');
const fields = document.getElementById('fields');
const submit = document.getElementById('submit');
const subtitle = document.getElementById('subtitle');
const links = document.getElementById('links');

function field(id, label, type) {
  const min = type === 'password' ? ' minlength="12"' : '';
  return `<label for="${id}">${label}</label><input id="${id}" name="${id}" type="${type}" required${min}>`;
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

if (mode === 'setup') {
  subtitle.textContent = 'Create the admin username and password for this cluster.';
  fields.innerHTML = field('username', 'Username', 'text')
    + field('password', 'Password (min 12)', 'password')
    + field('confirm', 'Confirm password', 'password');
  submit.textContent = 'Create admin';
} else if (mode === 'login') {
  subtitle.textContent = 'Sign in to the admin console.';
  fields.innerHTML = field('username', 'Username', 'text')
    + field('password', 'Password', 'password');
  submit.textContent = 'Sign in';
} else if (mode === 'password') {
  subtitle.textContent = 'Change the admin password. Other sessions will be signed out.';
  fields.innerHTML = field('current', 'Current password', 'password')
    + field('password', 'New password (min 12)', 'password')
    + field('confirm', 'Confirm new password', 'password');
  submit.textContent = 'Change password';
  links.innerHTML = '<a href="/">Back to console</a>';
}

async function loadStatus() {
  const res = await fetch('/api/auth/status', { credentials: 'same-origin' });
  if (!res.ok) {
    return null;
  }
  return res.json();
}

async function routeFromStatus() {
  const status = await loadStatus();
  if (!status) {
    return;
  }
  if (mode === 'setup' && status.configured) {
    window.location.replace('/login/');
    return true;
  }
  if (mode === 'login' && !status.configured) {
    window.location.replace('/setup/');
    return true;
  }
  if ((mode === 'login' || mode === 'setup') && status.authenticated) {
    window.location.replace('/');
    return true;
  }
  if (mode === 'password' && !status.authenticated) {
    window.location.replace(status.configured ? '/login/' : '/setup/');
    return true;
  }
  return false;
}

async function loadIdentity() {
  const box = document.getElementById('identity');
  try {
    const res = await fetch('/api/auth/identity', { credentials: 'same-origin' });
    const id = await res.json();
    const brand = document.querySelector('.auth-title');
    if (brand && id.product === 'skiff') {
      brand.textContent = 'Curio PDP';
    }
    const parts = ['<h2>This node</h2>'];
    if (id.providerId != null) {
      const domain = id.domain ? ` (${escapeHtml(id.domain)})` : '';
      parts.push(`<div class="row"><span class="label">Provider:</span> #${id.providerId}${domain}</div>`);
    }
    if (id.miners && id.miners.length) {
      parts.push(`<div class="row"><span class="label">Miner${id.miners.length > 1 ? 's' : ''}:</span> ${id.miners.join(', ')}</div>`);
    }
    if (id.providerId == null && !(id.miners && id.miners.length)) {
      parts.push('<div class="row muted">No miner or provider configured yet.</div>');
    }
    box.innerHTML = parts.join('');
  } catch (_) {
    box.innerHTML = '<h2>This node</h2><div class="muted">Could not load identity.</div>';
  }
}

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  errEl.textContent = '';
  submit.disabled = true;
  const body = {};
  if (mode === 'setup' || mode === 'login') {
    body.username = document.getElementById('username').value;
    body.password = document.getElementById('password').value;
    if (mode === 'setup') {
      body.confirm = document.getElementById('confirm').value;
    }
  } else {
    body.current = document.getElementById('current').value;
    body.password = document.getElementById('password').value;
    body.confirm = document.getElementById('confirm').value;
  }
  let url = '/api/auth/login';
  if (mode === 'setup') url = '/api/auth/setup';
  if (mode === 'password') url = '/api/auth/password';
  try {
    const res = await fetch(url, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const text = await res.text();
    let msg = text;
    try {
      const j = JSON.parse(text);
      msg = j.error || j.message || text;
    } catch (_) {}
    if (res.status === 429) {
      errEl.textContent = msg || 'Wait 5 seconds before trying again.';
      return;
    }
    if (!res.ok) {
      errEl.textContent = msg || (`Request failed (${res.status})`);
      return;
    }
    window.location.href = '/';
  } catch (err) {
    errEl.textContent = String(err);
  } finally {
    submit.disabled = false;
  }
});

(async () => {
  if (await routeFromStatus()) {
    return;
  }
  await loadIdentity();
})();
