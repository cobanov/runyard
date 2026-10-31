<script>
  // Text samples (run.text): a JSON list of rows, shown as a table.
  let { url } = $props()
  let rows = $state([])
  $effect(() => {
    fetch(url).then((r) => r.json()).then((d) => (rows = d))
  })
  const cols = $derived(rows.length ? Object.keys(rows[0]) : [])
</script>

{#if rows.length}
  <div class="table">
    <table>
      <thead><tr>{#each cols as c}<th>{c}</th>{/each}</tr></thead>
      <tbody>{#each rows as row}<tr>{#each cols as c}<td>{row[c]}</td>{/each}</tr>{/each}</tbody>
    </table>
  </div>
{/if}

<style>
  .table {
    overflow-x: auto;
    margin-bottom: 8px;
  }
  table {
    border-collapse: collapse;
    width: 100%;
    font-size: 0.875rem;
  }
  th,
  td {
    text-align: left;
    vertical-align: top;
    padding: 6px 8px;
    border-bottom: 1px solid var(--rule);
    white-space: pre-wrap;
  }
  th {
    color: var(--muted);
  }
</style>
