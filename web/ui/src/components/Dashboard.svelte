<script>
  export let status = {}
  export let batchStatus = { running: false, paused: false, auto: false }
  export let onBatchAction = () => {}
</script>

<div class="dashboard">
  <div class="stats-grid">
    <div class="stat-card">
      <span class="value">{status.agents_running || 0}</span>
      <span class="label">Running</span>
    </div>
    <div class="stat-card">
      <span class="value">{status.complete || 0}/{status.total || 0}</span>
      <span class="label">Complete</span>
    </div>
    <div class="stat-card">
      <span class="value">{status.in_progress || 0}</span>
      <span class="label">In Progress</span>
    </div>
    <div class="stat-card">
      <span class="value">{status.not_started || 0}</span>
      <span class="label">Queued</span>
    </div>
  </div>

  <div class="batch-controls">
    <h3>Batch Execution</h3>
    <div class="batch-status">
      {#if batchStatus.running}
        {#if batchStatus.paused}
          <span class="badge paused">Paused</span>
        {:else}
          <span class="badge running">Running</span>
        {/if}
      {:else}
        <span class="badge idle">Idle</span>
      {/if}
    </div>
    <div class="batch-buttons">
      {#if !batchStatus.running}
        <button class="btn primary" on:click={() => onBatchAction('start')}>Start</button>
      {:else if batchStatus.paused}
        <button class="btn primary" on:click={() => onBatchAction('resume')}>Resume</button>
        <button class="btn" on:click={() => onBatchAction('stop')}>Stop</button>
      {:else}
        <button class="btn" on:click={() => onBatchAction('pause')}>Pause</button>
        <button class="btn danger" on:click={() => onBatchAction('stop')}>Stop</button>
      {/if}
    </div>
    <label class="auto-toggle">
      <input type="checkbox" checked={batchStatus.auto} on:change={() => onBatchAction('auto')} />
      Auto Mode
    </label>
  </div>
</div>

<style>
  .dashboard {
    padding: 16px;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 12px;
    margin-bottom: 24px;
  }

  .stat-card {
    background: #f5f5f5;
    border-radius: 8px;
    padding: 16px;
    text-align: center;
  }

  .stat-card .value {
    display: block;
    font-size: 24px;
    font-weight: bold;
    color: #333;
  }

  .stat-card .label {
    font-size: 12px;
    color: #666;
  }

  .batch-controls {
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    padding: 16px;
  }

  .batch-controls h3 {
    margin: 0 0 12px 0;
    font-size: 16px;
  }

  .batch-status {
    margin-bottom: 12px;
  }

  .badge {
    display: inline-block;
    padding: 4px 12px;
    border-radius: 12px;
    font-size: 12px;
    font-weight: 500;
  }

  .badge.running { background: #e3f2fd; color: #1976d2; }
  .badge.paused { background: #fff3e0; color: #f57c00; }
  .badge.idle { background: #f5f5f5; color: #666; }

  .batch-buttons {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
  }

  .btn {
    padding: 10px 20px;
    border: 1px solid #ddd;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 14px;
  }

  .btn.primary {
    background: #0066cc;
    color: #fff;
    border-color: #0066cc;
  }

  .btn.danger {
    color: #d32f2f;
    border-color: #d32f2f;
  }

  .auto-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
  }

  @media (min-width: 768px) {
    .stats-grid {
      grid-template-columns: repeat(4, 1fr);
    }
  }
</style>
