<script>
    import { onMount, onDestroy } from 'svelte';
    import { fetchStatus, fetchTasks, createEventSource } from './lib/api.js';

    let status = { total: 0, not_started: 0, in_progress: 0, complete: 0, agents_running: 0 };
    let tasks = [];
    let eventSource = null;

    onMount(async () => {
        status = await fetchStatus();
        tasks = await fetchTasks();

        // Connect to SSE
        eventSource = createEventSource();
        eventSource.onmessage = (event) => {
            const data = JSON.parse(event.data);
            handleEvent(data);
        };
    });

    onDestroy(() => {
        if (eventSource) {
            eventSource.close();
        }
    });

    function handleEvent(event) {
        if (event.type === 'status_update') {
            status = event.data;
        } else if (event.type === 'task_update') {
            const idx = tasks.findIndex(t => t.id === event.data.id);
            if (idx >= 0) {
                tasks[idx] = event.data;
                tasks = tasks;
            }
        }
    }

    function statusEmoji(s) {
        switch (s) {
            case 'not_started': return '🔴';
            case 'in_progress': return '🟡';
            case 'complete': return '🟢';
            default: return '⚪';
        }
    }
</script>

<main>
    <header>
        <h1>ERP Orchestrator</h1>
        <div class="stats">
            <span>Total: {status.total}</span>
            <span>🔴 {status.not_started}</span>
            <span>🟡 {status.in_progress}</span>
            <span>🟢 {status.complete}</span>
            <span>Agents: {status.agents_running}</span>
        </div>
    </header>

    <section class="tasks">
        <h2>Tasks</h2>
        <table>
            <thead>
                <tr>
                    <th>ID</th>
                    <th>Title</th>
                    <th>Status</th>
                    <th>Priority</th>
                </tr>
            </thead>
            <tbody>
                {#each tasks as task}
                    <tr>
                        <td>{task.id}</td>
                        <td>{task.title}</td>
                        <td>{statusEmoji(task.status)}</td>
                        <td>{task.priority || '-'}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </section>
</main>

<style>
    main {
        max-width: 1200px;
        margin: 0 auto;
        padding: 1rem;
        font-family: system-ui, sans-serif;
    }

    header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 2rem;
        padding-bottom: 1rem;
        border-bottom: 1px solid #ddd;
    }

    .stats {
        display: flex;
        gap: 1rem;
    }

    .stats span {
        padding: 0.5rem 1rem;
        background: #f5f5f5;
        border-radius: 4px;
    }

    table {
        width: 100%;
        border-collapse: collapse;
    }

    th, td {
        text-align: left;
        padding: 0.75rem;
        border-bottom: 1px solid #eee;
    }

    th {
        background: #f9f9f9;
        font-weight: 600;
    }

    tr:hover {
        background: #f5f5f5;
    }
</style>
