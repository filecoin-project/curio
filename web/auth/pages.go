package auth

import (
	"net/http"
)

const authPageCSS = `
html,body{margin:0;min-height:100vh;background:#0d1117;color:#e6edf3;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;color-scheme:dark}
.wrap{max-width:420px;margin:0 auto;padding:48px 20px}
h1{font-size:22px;font-weight:600;margin:0 0 8px}
.sub{color:#8b949e;font-size:13px;margin:0 0 24px}
.card{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:20px}
label{display:block;font-size:12px;color:#8b949e;margin:12px 0 4px}
input{width:100%;box-sizing:border-box;background:#0d1117;border:1px solid #30363d;border-radius:6px;color:#e6edf3;padding:8px 10px;font-size:14px}
button{margin-top:16px;width:100%;background:#5e6ad2;color:#fff;border:0;border-radius:6px;padding:10px 12px;font-size:14px;font-weight:500;cursor:pointer}
button:disabled{opacity:.6;cursor:not-allowed}
.err{color:#f85149;font-size:13px;margin-top:12px;min-height:18px}
.identity{background:#0d1117;border:1px solid #30363d;border-radius:8px;padding:12px 14px;margin-bottom:20px;font-size:12px;line-height:1.5}
.identity h2{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:#8b949e;margin:0 0 8px}
.identity .row{margin:4px 0;word-break:break-all}
.identity .muted{color:#656d76}
.identity .label{color:#8b949e}
a{color:#5e6ad2;text-decoration:none;font-size:13px}
.links{margin-top:16px;text-align:center}
`

func writeAuthPage(w http.ResponseWriter, title, mode string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + title + ` — Curio</title>
<style>` + authPageCSS + `</style>
</head><body>
<div class="wrap">
  <h1>` + title + `</h1>
  <p class="sub" id="subtitle"></p>
  <div class="identity" id="identity"><h2>This node</h2><div class="muted">Loading…</div></div>
  <div class="card">
    <form id="form" autocomplete="on">
      <div id="fields"></div>
      <button type="submit" id="submit">Continue</button>
      <div class="err" id="err"></div>
    </form>
  </div>
  <div class="links" id="links"></div>
</div>
<script>
const mode = ` + "`" + mode + "`" + `;
const errEl = document.getElementById('err');
const form = document.getElementById('form');
const fields = document.getElementById('fields');
const submit = document.getElementById('submit');
const subtitle = document.getElementById('subtitle');
const links = document.getElementById('links');

function field(id, label, type) {
  return '<label for="'+id+'">'+label+'</label><input id="'+id+'" name="'+id+'" type="'+type+'" required'+(type==='password'?' minlength="12"':'')+'>';
}

if (mode === 'setup') {
  subtitle.textContent = 'Create the admin username and password for this cluster.';
  fields.innerHTML = field('username','Username','text') + field('password','Password (min 12)','password') + field('confirm','Confirm password','password');
  submit.textContent = 'Create admin';
} else if (mode === 'login') {
  subtitle.textContent = 'Sign in to the admin console.';
  fields.innerHTML = field('username','Username','text') + field('password','Password','password');
  submit.textContent = 'Sign in';
} else if (mode === 'password') {
  subtitle.textContent = 'Change the admin password. Other sessions will be signed out.';
  fields.innerHTML = field('current','Current password','password') + field('password','New password (min 12)','password') + field('confirm','Confirm new password','password');
  submit.textContent = 'Change password';
  links.innerHTML = '<a href="/">Back to console</a>';
}

async function loadIdentity() {
  const box = document.getElementById('identity');
  try {
    const res = await fetch('/api/auth/identity', {credentials:'same-origin'});
    const id = await res.json();
    const parts = [];
    parts.push('<h2>This node</h2>');
    parts.push('<div class="row"><span class="label">Product:</span> '+(id.product||'curio')+'</div>');
    if (id.providerId != null) {
      parts.push('<div class="row"><span class="label">Provider ID:</span> #'+id.providerId+'</div>');
    }
    if (id.pdpWallet && id.pdpWallet.address) {
      parts.push('<div class="row"><span class="label">PDP wallet:</span> '+id.pdpWallet.address+(id.pdpWallet.filAddress?' ('+id.pdpWallet.filAddress+')':'')+'</div>');
    }
    if (id.miners && id.miners.length) {
      id.miners.forEach((m, i) => {
        const miners = (m.minerAddresses||[]).join(', ') || '(none)';
        parts.push('<div class="row"><span class="label">Miner'+(id.miners.length>1?(' '+(i+1)):'')+':</span> '+miners+'</div>');
        const controls = [].concat(m.preCommitControl||[], m.commitControl||[], m.dealPublishControl||[], m.terminateControl||[]);
        if (controls.length) {
          parts.push('<div class="row"><span class="label">Control wallets:</span> '+controls.join(', ')+'</div>');
        }
      });
    }
    if (!id.providerId && !(id.pdpWallet&&id.pdpWallet.address) && !(id.miners&&id.miners.length)) {
      parts.push('<div class="row muted">No miner or provider identity configured yet.</div>');
    }
    box.innerHTML = parts.join('');
  } catch (e) {
    box.innerHTML = '<h2>This node</h2><div class="muted">Could not load identity.</div>';
  }
}
loadIdentity();

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  errEl.textContent = '';
  submit.disabled = true;
  const body = {};
  if (mode === 'setup' || mode === 'login') {
    body.username = document.getElementById('username').value;
    body.password = document.getElementById('password').value;
    if (mode === 'setup') body.confirm = document.getElementById('confirm').value;
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
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify(body),
    });
    const text = await res.text();
    let msg = text;
    try { const j = JSON.parse(text); msg = j.error || j.message || text; } catch (_) {}
    if (res.status === 429) {
      errEl.textContent = msg || 'Wait 5 seconds before trying again.';
      return;
    }
    if (!res.ok) {
      errEl.textContent = msg || ('Request failed ('+res.status+')');
      return;
    }
    window.location.href = mode === 'password' ? '/' : '/';
  } catch (err) {
    errEl.textContent = String(err);
  } finally {
    submit.disabled = false;
  }
});
</script>
</body></html>`))
}

func (a *Auth) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	ok, err := a.store.HasAdmin(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	writeAuthPage(w, "Admin setup", "setup")
}

func (a *Auth) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	ok, err := a.store.HasAdmin(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	if a.sessionAuthenticated(r.Context(), r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	writeAuthPage(w, "Admin login", "login")
}

func (a *Auth) handlePasswordPage(w http.ResponseWriter, r *http.Request) {
	writeAuthPage(w, "Change password", "password")
}
