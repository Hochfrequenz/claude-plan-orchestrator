<script>
  export let groups = []
  export let onPriorityChange = () => {}

  // Group by priority tier
  $: groupedByTier = groups.reduce((acc, g) => {
    const tier = g.priority < 0 ? 'unassigned' : g.priority
    if (!acc[tier]) acc[tier] = []
    acc[tier].push(g)
    return acc
  }, {})

  $: sortedTiers = Object.keys(groupedByTier)
    .filter(t => t !== 'unassigned')
    .map(Number)
    .sort((a, b) => a - b)

  function changePriority(group, delta) {
    const newPriority = Math.max(0, group.priority + delta)
    onPriorityChange(group.name, newPriority)
  }

  function unassign(group) {
    onPriorityChange(group.name, -1)
  }
</script>

<div class="groups">
  <h2>Group Priorities</h2>

  {#each sortedTiers as tier}
    <div class="tier">
      <h3>Tier {tier} {tier === 0 ? '(runs first)' : ''}</h3>
      {#each groupedByTier[tier] as group}
        <div class="group-row">
          <span class="group-name">{group.name}</span>
          <div class="progress-bar">
            <div class="progress-fill" style="width: {(group.completed / group.total) * 100}%"></div>
          </div>
          <span class="progress-text">{group.completed}/{group.total}</span>
          <div class="priority-controls">
            <button class="tier-btn" on:click={() => changePriority(group, -1)} disabled={group.priority === 0}>↑</button>
            <button class="tier-btn" on:click={() => changePriority(group, 1)}>↓</button>
            <button class="tier-btn unassign" on:click={() => unassign(group)}>×</button>
          </div>
        </div>
      {/each}
    </div>
  {/each}

  {#if groupedByTier['unassigned']?.length > 0}
    <div class="tier unassigned">
      <h3>Unassigned</h3>
      {#each groupedByTier['unassigned'] as group}
        <div class="group-row">
          <span class="group-name">{group.name}</span>
          <div class="progress-bar">
            <div class="progress-fill" style="width: {(group.completed / group.total) * 100}%"></div>
          </div>
          <span class="progress-text">{group.completed}/{group.total}</span>
          <div class="priority-controls">
            <button class="tier-btn" on:click={() => onPriorityChange(group.name, 0)}>+ Add to Tier 0</button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .groups {
    padding: 16px;
  }

  h2 {
    margin: 0 0 16px 0;
    font-size: 18px;
  }

  .tier {
    margin-bottom: 24px;
  }

  .tier h3 {
    font-size: 14px;
    color: #666;
    margin: 0 0 8px 0;
  }

  .group-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px;
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    margin-bottom: 8px;
  }

  .group-name {
    font-weight: 500;
    min-width: 100px;
  }

  .progress-bar {
    flex: 1;
    height: 8px;
    background: #eee;
    border-radius: 4px;
    overflow: hidden;
  }

  .progress-fill {
    height: 100%;
    background: #4caf50;
    transition: width 0.3s;
  }

  .progress-text {
    font-size: 13px;
    color: #666;
    min-width: 50px;
    text-align: right;
  }

  .priority-controls {
    display: flex;
    gap: 4px;
  }

  .tier-btn {
    width: 32px;
    height: 32px;
    border: 1px solid #ddd;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 14px;
  }

  .tier-btn:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .tier-btn.unassign {
    color: #d32f2f;
  }

  .unassigned .tier-btn {
    width: auto;
    padding: 0 12px;
    font-size: 12px;
  }
</style>
