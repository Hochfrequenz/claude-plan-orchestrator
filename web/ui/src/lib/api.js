const API_BASE = '/api'

export async function fetchStatus() {
  const res = await fetch(`${API_BASE}/status`)
  return res.json()
}

export async function fetchTasks(params) {
  const url = new URL(`${API_BASE}/tasks`, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([k, v]) => url.searchParams.set(k, v))
  }
  const res = await fetch(url)
  return res.json()
}

export async function fetchTask(id) {
  const res = await fetch(`${API_BASE}/tasks/${encodeURIComponent(id)}`)
  return res.json()
}

// Batch control
export async function fetchBatchStatus() {
  const res = await fetch(`${API_BASE}/batch/status`)
  return res.json()
}

export async function batchStart() {
  const res = await fetch(`${API_BASE}/batch/start`, { method: 'POST' })
  return res.json()
}

export async function batchStop() {
  const res = await fetch(`${API_BASE}/batch/stop`, { method: 'POST' })
  return res.json()
}

export async function batchPause() {
  const res = await fetch(`${API_BASE}/batch/pause`, { method: 'POST' })
  return res.json()
}

export async function batchResume() {
  const res = await fetch(`${API_BASE}/batch/resume`, { method: 'POST' })
  return res.json()
}

export async function batchToggleAuto() {
  const res = await fetch(`${API_BASE}/batch/auto`, { method: 'POST' })
  return res.json()
}

// Agents
export async function fetchAgents() {
  const res = await fetch(`${API_BASE}/agents`)
  return res.json()
}

export async function stopAgent(taskId) {
  const res = await fetch(`${API_BASE}/agents/${encodeURIComponent(taskId)}/stop`, { method: 'POST' })
  return res.json()
}

export async function resumeAgent(taskId) {
  const res = await fetch(`${API_BASE}/agents/${encodeURIComponent(taskId)}/resume`, { method: 'POST' })
  return res.json()
}

export async function fetchAgentLogs(taskId) {
  const res = await fetch(`${API_BASE}/agents/${encodeURIComponent(taskId)}/logs`)
  return res.json()
}

// PRs
export async function fetchPRs() {
  const res = await fetch(`${API_BASE}/prs`)
  return res.json()
}

export async function mergePR(prNumber) {
  const res = await fetch(`${API_BASE}/prs/${prNumber}/merge`, { method: 'POST' })
  return res.json()
}

// Groups
export async function fetchGroups() {
  const res = await fetch(`${API_BASE}/groups`)
  return res.json()
}

export async function setGroupPriority(name, priority) {
  const res = await fetch(`${API_BASE}/groups/${encodeURIComponent(name)}/priority`, {
    method: priority < 0 ? 'DELETE' : 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: priority >= 0 ? JSON.stringify({ priority }) : undefined,
  })
  return res.json()
}

// SSE
export function createEventSource() {
  return new EventSource(`${API_BASE}/events`)
}
