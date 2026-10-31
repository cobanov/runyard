<script>
  import { get, send, why, store, ago, live, cred, setKey } from './lib.svelte.js'

  let { auth } = $props()
  const origin = location.origin

  let keys = $state(null)
  let channels = $state(null)
  let errors = $state({}) // per section: keys, channels
  let made = $state(null) // {name, key}: a new key, shown this once
  let tested = $state({}) // channel id -> 'Sent' or why not
  let copied = $state('')

  const loadKeys = () => get('/keys').then((k) => (keys = k))
  const loadChannels = () => get('/channels').then((c) => (channels = c))
  // act makes a change for a section and reloads it; a failure shows in that section.
  async function act(section, change, reload) {
    errors[section] = ''
    try {
      await change()
      await reload()
    } catch (e) {
      if (!auth && e.status === 401 && cred.key) return void ((keyError = 'The server refused that key.'), setKey(''))
      errors[section] = why(e)
    }
  }
  // Without users the server needs an API key for these; with them, guard and the login form have it covered.
  $effect(() => {
    if (!auth && !cred.key) return
    act('keys', () => {}, loadKeys)
    act('channels', () => {}, loadChannels)
  })
  let typed = $state('')
  let keyError = $state('')
  const useKey = (e) => {
    e.preventDefault()
    keyError = ''
    setKey(typed.trim())
    typed = ''
  }

  const key = $derived(made?.key || 'ry_...')
  const snippets = $derived([
    ['Python SDK', `export RUNYARD_SERVER=${origin} RUNYARD_API_KEY=${key}`],
    ['MLflow client', `export MLFLOW_TRACKING_URI=${origin} MLFLOW_TRACKING_TOKEN=${key}`],
    ['MCP for agents (Claude Code)', `claude mcp add --transport http runyard ${origin}/mcp --header "Authorization: Bearer ${key}"`],
  ])
  const copy = (text) => navigator.clipboard.writeText(text).then(() => (copied = text))

  let keyName = $state('')
  let keyUser = $state('')
  const createKey = (e) => {
    e.preventDefault()
    act('keys', async () => {
      made = { name: keyName, ...(await send('POST', '/keys', { name: keyName, user: keyUser })) }
      keyName = keyUser = ''
    }, loadKeys)
  }
  const revoke = (k) =>
    confirm(`Revoke the key ${k.name}? Scripts and agents using it stop working.`) && act('keys', () => send('DELETE', '/keys/' + k.id), loadKeys)

  const kinds = { ntfy: 'ntfy', webhook: 'Webhook', slack: 'Slack', discord: 'Discord', telegram: 'Telegram' }
  const hints = { ntfy: 'https://ntfy.sh/my-topic', webhook: 'https://example.com/hook', slack: 'https://hooks.slack.com/services/…', discord: 'https://discord.com/api/webhooks/…', telegram: '123456789' }
  const events = ['finished', 'failed', 'crashed', 'alert']
  let ch = $state({ kind: 'ntfy', target: '', token: '', project: '', events: [...events] })
  const tokened = $derived(ch.kind === 'ntfy' || ch.kind === 'telegram')
  const addChannel = (e) => {
    e.preventDefault()
    act('channels', async () => {
      await send('POST', '/channels', { ...ch, token: tokened ? ch.token : '' })
      ch = { ...ch, target: '', token: '' }
    }, loadChannels)
  }
  async function test(c) {
    tested[c.id] = 'Sending…'
    tested[c.id] = await send('POST', `/channels/${c.id}/test`).then(() => 'Sent', why)
  }
  const remove = (c) => confirm(`Remove this ${kinds[c.kind] || c.kind} channel?`) && act('channels', () => send('DELETE', '/channels/' + c.id), loadChannels)
  const target = (c) => (c.kind === 'webpush' ? new URL(c.target).host : c.target) // a push endpoint is long; its host names the browser
</script>

<div class="page-head"><h1>Settings</h1></div>

{#if !auth}
  <section>
    <h2>Access</h2>
    {#if cred.key}
      <div class="card card-pad access">
        <p class="small">Changes from this tab use the API key you added. It is forgotten when the tab closes.</p>
        <button onclick={() => ((keys = channels = null), setKey(''))}>Forget the key</button>
      </div>
    {:else}
      <div class="card card-pad access">
        <p class="small">
          This server runs without users (<code>RUNYARD_AUTH</code> is off): anyone who reaches it can read, so API keys,
          notification channels and deleting runs need an API key. Add one to manage them here, or use the server's command line.
        </p>
        <form class="add" onsubmit={useKey}>
          <label class="grow">API key <input class="field" type="password" bind:value={typed} placeholder="ry_..." autocomplete="off" required /></label>
          <button class="btn-primary">Use key</button>
        </form>
        {#if keyError}<p class="small"><span class="danger">{keyError}</span></p>{/if}
        <pre class="small muted">docker compose exec app /runyard-server key create my-laptop
docker compose exec app /runyard-server channel add ntfy https://ntfy.sh/my-topic</pre>
        <p class="small muted">For logins and teams, start the server with <code>RUNYARD_AUTH=1</code> and add an admin with <code>runyard-server user add -admin NAME</code>.</p>
      </div>
    {/if}
  </section>
{/if}

<section>
  <h2>Connect</h2>
  <ul class="card row-list">
    {#each snippets as [label, text]}
      <li class="snip">
        <span class="muted small">{label}</span>
        <code>{text}</code>
        {#if navigator.clipboard}<button onclick={() => copy(text)}>{copied === text ? 'Copied' : 'Copy'}</button>{/if}
      </li>
    {/each}
  </ul>
  <p class="muted small">{made ? 'These use the key you just made.' : 'Make an API key below and the commands fill it in.'} MCP gives agents the runs, metrics, logs and samples to read.</p>
</section>

{#if auth || cred.key}
<section>
  <h2>API keys</h2>
  {#if errors.keys}
    <p class="empty"><span class="danger">Failed.</span> {errors.keys}</p>
  {/if}
  {#if keys}
    {#if made}
      <div class="card card-pad made">
        <p class="small">The key <strong>{made.name}</strong>. Copy it now: the server keeps only its hash.</p>
        <div class="snip"><code>{made.key}</code>{#if navigator.clipboard}<button onclick={() => copy(made.key)}>{copied === made.key ? 'Copied' : 'Copy'}</button>{/if}</div>
      </div>
    {/if}
    <form class="add" onsubmit={createKey}>
      <label>Name <input class="field" bind:value={keyName} placeholder="my-laptop" required /></label>
      {#if auth}<label>Acts as user <input class="field" bind:value={keyUser} placeholder="none: sees everything" autocapitalize="none" /></label>{/if}
      <button class="btn-primary">Create key</button>
    </form>
    {#if keys.length}
      <div class="card table">
        <table>
          <thead><tr><th>Name</th><th>User</th><th>Created</th><th><span class="sr">Revoke</span></th></tr></thead>
          <tbody>
            {#each keys as k (k.id)}
              <tr>
                <td class="strong">{k.name}</td>
                <td>{#if k.user}{k.user}{:else}<span class="muted">server key</span>{/if}</td>
                <td class="muted">{ago(k.created_at, live.now)}</td>
                <td class="end"><button class="danger" onclick={() => revoke(k)}>Revoke</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="empty">No API keys yet.</p>
    {/if}
  {/if}
</section>

<section>
  <h2>Notifications</h2>
  {#if errors.channels}
    <p class="empty"><span class="danger">Failed.</span> {errors.channels}</p>
  {/if}
  {#if channels}
    <form class="add" onsubmit={addChannel}>
      <label>Kind
        <select class="field" bind:value={ch.kind}>{#each Object.entries(kinds) as [k, label]}<option value={k}>{label}</option>{/each}</select>
      </label>
      <label class="grow">{ch.kind === 'telegram' ? 'Chat id' : 'URL'} <input class="field" bind:value={ch.target} placeholder={hints[ch.kind]} required /></label>
      {#if tokened}
        <label>{ch.kind === 'telegram' ? 'Bot token' : 'Access token (optional)'}
          <input class="field" type="password" bind:value={ch.token} required={ch.kind === 'telegram'} autocomplete="off" />
        </label>
      {/if}
      <label>Project
        <select class="field" bind:value={ch.project}>
          <option value="">Every project</option>
          {#each store.projects || [] as p (p.name)}<option>{p.name}</option>{/each}
        </select>
      </label>
      <div class="events" role="group" aria-label="Events">
        {#each events as e}<label class="check"><input type="checkbox" bind:group={ch.events} value={e} /> {e}</label>{/each}
      </div>
      <button disabled={!ch.events.length}>Add channel</button>
    </form>
    {#if channels.length}
      <div class="card table">
        <table>
          <thead><tr><th>Kind</th><th>Sends to</th><th>Project</th><th>Events</th><th><span class="sr">Actions</span></th></tr></thead>
          <tbody>
            {#each channels as c}
              <tr>
                <td class="strong">{c.kind === 'webpush' ? 'Web push' : kinds[c.kind] || c.kind}</td>
                <td class="wrap">{target(c)}</td>
                <td>{#if c.project}{c.project}{:else}<span class="muted">every project</span>{/if}</td>
                <td class="muted">{c.events.join(', ')}</td>
                <td class="end">
                  {#if c.kind === 'webpush'}
                    <span class="muted small">a browser's bell</span>
                  {:else}
                    {#if tested[c.id]}<span class="muted small">{tested[c.id]}</span>{/if}
                    <button onclick={() => test(c)}>Test</button>
                    <button class="danger" onclick={() => remove(c)}>Remove</button>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="empty">No channels yet. The bell in the sidebar adds this browser.</p>
    {/if}
  {/if}
</section>
{/if}

<style>
  section {
    margin-top: 24px;
  }
  .access {
    display: grid;
    gap: 10px;
    justify-items: start;
  }
  .access p,
  .access pre {
    margin: 0;
  }
  .access pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .access .add {
    width: 100%;
  }
  section .empty {
    margin: 0 0 14px;
    padding: 8px 14px;
    border: 1px dashed var(--rule);
    border-radius: var(--radius);
    max-width: none;
  }
  .snip {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 12px;
    padding: 10px 14px;
  }
  .snip .muted {
    flex: 0 0 100%;
  }
  code {
    flex: 1;
    min-width: 0;
    font-family: var(--mono);
    font-size: 0.8125rem;
    overflow-wrap: anywhere;
  }
  .made {
    margin-bottom: 14px;
  }
  .made p {
    margin: 0;
  }
  .made .snip {
    padding: 8px 0 0;
  }
  .add {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 14px;
    margin: 0 0 14px;
  }
  .add > label {
    display: grid;
    gap: 4px;
    font-size: 0.8125rem;
    color: var(--muted);
  }
  .add input,
  .add select {
    color: var(--ink);
    font-size: 0.9375rem;
  }
  .grow {
    flex: 1 1 260px;
  }
  .events {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    min-height: 40px;
    align-items: center;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 0.875rem;
  }
  .check input {
    margin: 0;
    accent-color: var(--ink);
  }
  .strong {
    font-weight: 600;
  }
  .wrap {
    white-space: normal;
    overflow-wrap: anywhere;
    min-width: 200px;
  }
  .end {
    text-align: right;
  }
  .end button + button {
    margin-left: 6px;
  }
  .end .small {
    margin-right: 8px;
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
</style>
