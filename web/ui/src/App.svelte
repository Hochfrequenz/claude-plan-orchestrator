<script>
  import { onMount, onDestroy } from 'svelte'
  import BottomNav from './components/BottomNav.svelte'
  import Dashboard from './components/Dashboard.svelte'
  import Agents from './components/Agents.svelte'
  import PRs from './components/PRs.svelte'
  import Groups from './components/Groups.svelte'
  import {
    fetchStatus,
    fetchBatchStatus,
    fetchAgents,
    fetchPRs,
    fetchGroups,
    fetchAgentLogs,
    batchStart,
    batchStop,
    batchPause,
    batchResume,
    batchToggleAuto,
    stopAgent,
    resumeAgent,
    mergePR,
    setGroupPriority,
    createEventSource
  } from './lib/api.js'

  let activeTab = 'dashboard'
  let status = {}
  let batchStatus = { running: false, paused: false, auto: false }
  let agents = []
  let prs = []
  let groups = []
  let eventSource = null

  // Logs page state
  let currentRoute = typeof window !== 'undefined' ? window.location.pathname : '/'
  let logsTaskId = null
  let logsContent = []
  let logsLoading = false

  onMount(async () => {
    if (currentRoute.startsWith('/logs/')) {
      logsTaskId = decodeURIComponent(currentRoute.replace('/logs/', ''))
      await loadLogs()
    } else {
      await loadData()
      setupSSE()
    }
  })

  onDestroy(() => {
    if (eventSource) {
      eventSource.close()
    }
  })

  async function loadData() {
    const results = await Promise.all([
      fetchStatus(),
      fetchBatchStatus().catch(() => ({ running: false, paused: false, auto: false })),
      fetchAgents().catch(() => []),
      fetchPRs().catch(() => []),
      fetchGroups().catch(() => []),
    ])
    status = results[0]
    batchStatus = results[1]
    agents = results[2]
    prs = results[3]
    groups = results[4]
  }

  async function loadLogs() {
    logsLoading = true
    try {
      const data = await fetchAgentLogs(logsTaskId)
      logsContent = data.lines || []
    } catch (e) {
      logsContent = ['Error loading logs: ' + e.message]
    }
    logsLoading = false
  }

  function setupSSE() {
    eventSource = createEventSource()
    eventSource.onmessage = (event) => {
      const data = JSON.parse(event.data)
      handleEvent(data)
    }
    eventSource.onerror = () => {
      setTimeout(setupSSE, 5000)
    }
  }

  function handleEvent(event) {
    switch (event.type) {
      case 'status_update':
        status = event.data
        break
      case 'batch_update':
        batchStatus = event.data
        break
      case 'agent_update':
        const idx = agents.findIndex(a => a.task_id === event.data.task_id)
        if (idx >= 0) {
          agents[idx] = { ...agents[idx], ...event.data }
          agents = agents
        } else {
          agents = [...agents, event.data]
        }
        break
      case 'pr_update':
        if (event.data.status === 'merged') {
          prs = prs.filter(p => p.pr_number !== event.data.pr_number)
        }
        break
      case 'group_update':
        const gIdx = groups.findIndex(g => g.name === event.data.name)
        if (gIdx >= 0) {
          groups[gIdx].priority = event.data.priority
          groups = groups
        }
        break
    }
  }

  async function handleBatchAction(action) {
    switch (action) {
      case 'start': await batchStart(); break
      case 'stop': await batchStop(); break
      case 'pause': await batchPause(); break
      case 'resume': await batchResume(); break
      case 'auto': await batchToggleAuto(); break
    }
  }

  async function handleAgentAction(taskId, action) {
    if (action === 'stop') {
      await stopAgent(taskId)
    } else if (action === 'resume') {
      await resumeAgent(taskId)
    }
  }

  async function handleMergePR(prNumber) {
    await mergePR(prNumber)
  }

  async function handlePriorityChange(name, priority) {
    await setGroupPriority(name, priority)
    groups = await fetchGroups()
  }
</script>

{#if logsTaskId}
  <div class="logs-page">
    <header class="logs-header">
      <a href="/" class="back-btn">← Back</a>
      <h1>Logs: {logsTaskId}</h1>
    </header>
    <div class="logs-content">
      {#if logsLoading}
        <p>Loading...</p>
      {:else}
        {#each logsContent as line}
          <div class="log-line">{line}</div>
        {/each}
      {/if}
    </div>
  </div>
{:else}
  <div class="app" class:desktop={typeof window !== 'undefined' && window.innerWidth >= 768}>
    <BottomNav {activeTab} onTabChange={(tab) => activeTab = tab} />

    <main class="content">
      {#if activeTab === 'dashboard'}
        <Dashboard {status} {batchStatus} onBatchAction={handleBatchAction} />
      {:else if activeTab === 'agents'}
        <Agents {agents} onAgentAction={handleAgentAction} />
      {:else if activeTab === 'prs'}
        <PRs {prs} onMerge={handleMergePR} />
      {:else if activeTab === 'groups'}
        <Groups {groups} onPriorityChange={handlePriorityChange} />
      {/if}
    </main>
  </div>
{/if}

<style>
  :global(body) {
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    background: #f5f5f5;
  }

  .app {
    min-height: 100vh;
    padding-bottom: 72px;
  }

  .content {
    max-width: 800px;
    margin: 0 auto;
  }

  @media (min-width: 768px) {
    .app {
      display: flex;
      padding-bottom: 0;
    }

    .content {
      flex: 1;
      margin-left: 80px;
      max-width: none;
      padding: 16px;
    }
  }

  .logs-page {
    min-height: 100vh;
    background: #1e1e1e;
    color: #d4d4d4;
  }

  .logs-header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 16px;
    background: #2d2d2d;
    border-bottom: 1px solid #404040;
  }

  .logs-header h1 {
    margin: 0;
    font-size: 16px;
    font-weight: normal;
  }

  .back-btn {
    color: #569cd6;
    text-decoration: none;
  }

  .logs-content {
    padding: 16px;
    font-family: 'SF Mono', Monaco, 'Courier New', monospace;
    font-size: 12px;
    line-height: 1.5;
    overflow-x: auto;
  }

  .log-line {
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
