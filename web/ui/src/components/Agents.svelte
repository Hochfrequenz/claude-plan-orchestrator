<script>
  export let agents = []
  export let onAgentAction = () => {}

  let expandedId = null

  function toggleExpand(id) {
    expandedId = expandedId === id ? null : id
  }

  function getStatusColor(status) {
    switch (status) {
      case 'running': return '#1976d2'
      case 'completed': return '#388e3c'
      case 'failed': return '#d32f2f'
      case 'stuck': return '#f57c00'
      default: return '#666'
    }
  }
</script>

<div class="agents">
  <h2>Agents</h2>
  {#if agents.length === 0}
    <p class="empty">No active agents</p>
  {:else}
    {#each agents as agent}
      <div class="agent-card" class:expanded={expandedId === agent.id}>
        <button class="agent-header" on:click={() => toggleExpand(agent.id)}>
          <div class="agent-info">
            <span class="task-id">{agent.task_id}</span>
            <span class="status" style="color: {getStatusColor(agent.status)}">{agent.status}</span>
          </div>
          <span class="duration">{agent.duration}</span>
        </button>

        {#if expandedId === agent.id}
          <div class="agent-details">
            <div class="detail-row">
              <span>Tokens:</span>
              <span>{agent.tokens_input} in / {agent.tokens_output} out</span>
            </div>
            <div class="detail-row">
              <span>Cost:</span>
              <span>${agent.cost_usd.toFixed(4)}</span>
            </div>
            {#if agent.log_lines && agent.log_lines.length > 0}
              <div class="log-preview">
                {#each agent.log_lines.slice(-5) as line}
                  <div class="log-line">{line}</div>
                {/each}
              </div>
            {/if}
            <div class="agent-actions">
              {#if agent.status === 'running' || agent.status === 'stuck'}
                <button class="btn danger" on:click={() => onAgentAction(agent.task_id, 'stop')}>Stop</button>
              {/if}
              {#if agent.status === 'failed'}
                <button class="btn primary" on:click={() => onAgentAction(agent.task_id, 'resume')}>Resume</button>
              {/if}
              <a href="/logs/{encodeURIComponent(agent.task_id)}" target="_blank" class="btn">Full Logs</a>
            </div>
          </div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .agents {
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

  .agent-card {
    background: #fff;
    border: 1px solid #ddd;
    border-radius: 8px;
    margin-bottom: 8px;
    overflow: hidden;
  }

  .agent-header {
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

  .agent-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .task-id {
    font-weight: 500;
  }

  .status {
    font-size: 12px;
    text-transform: uppercase;
  }

  .duration {
    color: #666;
    font-size: 14px;
  }

  .agent-details {
    padding: 0 16px 16px;
    border-top: 1px solid #eee;
  }

  .detail-row {
    display: flex;
    justify-content: space-between;
    padding: 8px 0;
    font-size: 14px;
  }

  .log-preview {
    background: #1e1e1e;
    color: #d4d4d4;
    font-family: monospace;
    font-size: 11px;
    padding: 8px;
    border-radius: 4px;
    margin: 8px 0;
    max-height: 120px;
    overflow-y: auto;
  }

  .log-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .agent-actions {
    display: flex;
    gap: 8px;
    margin-top: 12px;
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

  .btn.primary {
    background: #0066cc;
    color: #fff;
    border-color: #0066cc;
  }

  .btn.danger {
    color: #d32f2f;
    border-color: #d32f2f;
  }
</style>
