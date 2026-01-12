const API_BASE = '/api';

export async function fetchStatus() {
    const res = await fetch(`${API_BASE}/status`);
    return res.json();
}

export async function fetchTasks(params = {}) {
    const query = new URLSearchParams(params).toString();
    const url = query ? `${API_BASE}/tasks?${query}` : `${API_BASE}/tasks`;
    const res = await fetch(url);
    return res.json();
}

export async function fetchTask(id) {
    const res = await fetch(`${API_BASE}/tasks/${id}`);
    return res.json();
}

export function createEventSource() {
    return new EventSource(`${API_BASE}/events`);
}
