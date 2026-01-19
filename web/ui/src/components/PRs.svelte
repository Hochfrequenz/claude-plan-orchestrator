<script>
  export let prs = []
  export let onMerge = () => {}

  let expandedId = null
  let confirmingMerge = null

  function toggleExpand(id) {
    expandedId = expandedId === id ? null : id
    confirmingMerge = null
  }

  function handleMerge(pr) {
    if (confirmingMerge === pr.pr_number) {
      onMerge(pr.pr_number)
      confirmingMerge = null
    } else {
      confirmingMerge = pr.pr_number
    }
  }

  function getFlagColor(reason) {
    switch (reason) {
      case 'security': return '#d32f2f'
      case 'architecture': return '#7b1fa2'
      case 'migration': return '#f57c00'
      default: return '#666'
    }
  }
</script>

<div class="prs">
  <h2>Flagged PRs</h2>
  {#if prs.length === 0}
    <p class="empty">No PRs need review</p>
  {:else}
    {#each prs as pr}
      <div class="pr-card" class:expanded={expandedId === pr.pr_number}>
        <button class="pr-header" on:click={() => toggleExpand(pr.pr_number)}>
          <div class="pr-info">
            <span class="pr-number">#{pr.pr_number}</span>
            <span class="task-id">{pr.task_id}</span>
          </div>
          <span class="flag-badge" style="background: {getFlagColor(pr.flag_reason)}">{pr.flag_reason}</span>
        </button>

        {#if expandedId === pr.pr_number}
          <div class="pr-details">
            <p class="pr-title">{pr.title}</p>
            <div class="pr-actions">
              <button
                class="btn"
                class:confirming={confirmingMerge === pr.pr_number}
                on:click={() => handleMerge(pr)}
              >
                {confirmingMerge === pr.pr_number ? 'Confirm Merge' : 'Merge'}
              </button>
              <a href={pr.url} target="_blank" class="btn">View on GitHub</a>
            </div>
            {#if confirmingMerge === pr.pr_number}
              <p class="confirm-hint">Tap again to confirm merge</p>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .prs {
    padding: 16px;
  }

  h2 {
    margin: 0 0 16px 0;
    font-size: 18px;
  }

  .empty {
    color: #666;
    text-align: center;
    padding: 32px;
  }

  .pr-card {
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    margin-bottom: 8px;
    overflow: hidden;
  }

  .pr-header {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    border: none;
    background: none;
    cursor: pointer;
    text-align: left;
  }

  .pr-info {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .pr-number {
    font-weight: 500;
  }

  .task-id {
    color: #666;
    font-size: 13px;
  }

  .flag-badge {
    color: #fff;
    font-size: 11px;
    padding: 3px 8px;
    border-radius: 10px;
    text-transform: uppercase;
  }

  .pr-details {
    padding: 0 16px 16px;
    border-top: 1px solid #eee;
  }

  .pr-title {
    margin: 12px 0;
    font-size: 14px;
  }

  .pr-actions {
    display: flex;
    gap: 8px;
  }

  .btn {
    padding: 8px 16px;
    border: 1px solid #ddd;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 13px;
    text-decoration: none;
    color: inherit;
  }

  .btn.confirming {
    background: #0066cc;
    color: #fff;
    border-color: #0066cc;
  }

  .confirm-hint {
    font-size: 12px;
    color: #666;
    margin-top: 8px;
  }
</style>
