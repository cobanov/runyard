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

<form onsubmit={submit}>
  <h1>Log in</h1>
  <label>Name <input bind:value={name} autocomplete="username" autocapitalize="none" required /></label>
  <label>Password <input type="password" bind:value={password} autocomplete="current-password" required /></label>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button disabled={busy}>Log in</button>
  <p class="muted small">Accounts are made on the server: <code>runyard-server user add NAME</code>.</p>
</form>

<style>
  form {
    display: grid;
    gap: 14px;
    max-width: 340px;
    padding-top: 24px;
  }
  label {
    display: grid;
    gap: 4px;
    font-weight: 700;
  }
  input {
    padding: 10px 12px;
    border: 1px solid var(--rule);
    border-radius: 6px;
    background: var(--panel);
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
