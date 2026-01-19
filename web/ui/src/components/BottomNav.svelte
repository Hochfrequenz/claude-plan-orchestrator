<script>
  export let activeTab = 'dashboard'
  export let onTabChange = () => {}

  const tabs = [
    { id: 'dashboard', label: 'Dashboard', icon: '📊' },
    { id: 'agents', label: 'Agents', icon: '🤖' },
    { id: 'prs', label: 'PRs', icon: '🔀' },
    { id: 'groups', label: 'Groups', icon: '📁' },
  ]
</script>

<nav class="bottom-nav">
  {#each tabs as tab}
    <button
      class="tab"
      class:active={activeTab === tab.id}
      on:click={() => onTabChange(tab.id)}
    >
      <span class="icon">{tab.icon}</span>
      <span class="label">{tab.label}</span>
    </button>
  {/each}
</nav>

<style>
  .bottom-nav {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    display: flex;
    background: #fff;
    border-top: 1px solid #ddd;
    padding: 8px 0;
    padding-bottom: max(8px, env(safe-area-inset-bottom));
    z-index: 100;
  }

  .tab {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 8px;
    border: none;
    background: none;
    cursor: pointer;
    color: #666;
    font-size: 12px;
  }

  .tab.active {
    color: #0066cc;
  }

  .icon {
    font-size: 20px;
  }

  .label {
    font-size: 11px;
  }

  @media (min-width: 768px) {
    .bottom-nav {
      position: static;
      flex-direction: column;
      width: 80px;
      height: 100vh;
      border-top: none;
      border-right: 1px solid #ddd;
      padding: 16px 0;
    }

    .tab {
      padding: 16px 8px;
    }
  }
</style>
