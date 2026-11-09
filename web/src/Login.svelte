<script>
  // Shown when the server runs with RUNYARD_AUTH and this browser has no session yet.
  let { onlogin } = $props()
  let name = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(e) {
    e.preventDefault()
    busy = true
    error = ''
    const r = await fetch('/api/v1/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, password }) })
    busy = false
    if (r.ok) onlogin((await r.json()).user)
    else error = (await r.json().catch(() => ({}))).error || r.statusText
  }
</script>

<form onsubmit={submit} class="card">
  <p class="brand"><svg viewBox="0 0 256 256" aria-hidden="true"><path fill="currentColor" d="M36 220 V144 A108 108 0 0 1 144 36 H220 L184 72 H144 A72 72 0 0 0 72 144 V220 Z" /><path fill="var(--running)" d="M96 220 V144 A48 48 0 0 1 144 96 H220 L184 132 H144 A12 12 0 0 0 132 144 V220 Z" /></svg>runyard</p>
  <h1>Log in</h1>
  <label>Name <input bind:value={name} autocomplete="username" autocapitalize="none" required /></label>
  <label>Password <input type="password" bind:value={password} autocomplete="current-password" required /></label>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button class="btn-primary" disabled={busy}>Log in</button>
  <p class="muted small">Accounts are made on the server: <code>runyard-server user add NAME</code>.</p>
</form>

<style>
  form {
    display: grid;
    gap: 14px;
    width: min(360px, calc(100vw - 32px));
    padding: 28px 24px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-weight: 600;
  }
  .brand svg {
    width: 20px;
    height: 20px;
  }
  label {
    display: grid;
    gap: 4px;
    font-weight: 500;
  }
  input {
    padding: 10px 12px;
    border: 1px solid var(--rule);
    border-radius: var(--radius-sm);
    background: var(--paper);
    font-weight: 400;
  }
  .error {
    margin: 0;
    color: var(--failed);
  }
  code {
    font-family: var(--mono);
  }
</style>
