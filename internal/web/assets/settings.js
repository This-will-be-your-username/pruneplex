const settingsForm = document.querySelector('#radarr-settings-form');
const settingsMessage = document.querySelector('#settings-message');
const saveSettingsButton = document.querySelector('#save-radarr-settings');

const instances = {
  '4k': {
    key: 'fourK',
    url: document.querySelector('#radarr-4k-url'),
    apiKey: document.querySelector('#radarr-4k-key'),
    state: document.querySelector('#radarr-4k-state'),
  },
  hd: {
    key: 'hd',
    url: document.querySelector('#radarr-hd-url'),
    apiKey: document.querySelector('#radarr-hd-key'),
    state: document.querySelector('#radarr-hd-state'),
  },
};

async function request(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options.headers },
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || 'The request could not be completed.');
  return data;
}

function showMessage(text, kind = '') {
  settingsMessage.textContent = text;
  settingsMessage.className = `settings-message ${kind}`.trim();
}

async function loadSettings() {
  const data = await request('/api/settings/radarr');
  for (const [quality, item] of Object.entries(instances)) {
    const saved = data[item.key];
    item.url.value = saved.url || '';
    item.state.textContent = saved.configured ? 'Configured' : 'Not configured';
    item.state.classList.toggle('configured', saved.configured);
    document.querySelector(`[data-clear="${quality}"]`).hidden = !saved.configured;
  }
}

settingsForm.addEventListener('submit', async (event) => {
  event.preventDefault();
  saveSettingsButton.disabled = true;
  showMessage('Saving settings…');
  try {
    await request('/api/settings/radarr', {
      method: 'PUT',
      body: JSON.stringify({
        fourK: { url: instances['4k'].url.value, apiKey: instances['4k'].apiKey.value },
        hd: { url: instances.hd.url.value, apiKey: instances.hd.apiKey.value },
      }),
    });
    for (const instance of Object.values(instances)) instance.apiKey.value = '';
    await loadSettings();
    showMessage('Radarr settings saved.', 'success');
  } catch (error) {
    showMessage(error.message, 'error');
  } finally {
    saveSettingsButton.disabled = false;
  }
});

document.querySelectorAll('[data-test]').forEach((button) => {
  button.addEventListener('click', async () => {
    const quality = button.dataset.test;
    const instance = instances[quality];
    const originalText = button.textContent;
    button.disabled = true;
    button.textContent = 'Testing…';
    showMessage(`Testing ${quality === '4k' ? '4K' : 'HD'} Radarr…`);
    try {
      await request('/api/settings/radarr/test', {
        method: 'POST',
        body: JSON.stringify({
          quality,
          instance: { url: instance.url.value, apiKey: instance.apiKey.value },
        }),
      });
      showMessage(`${quality === '4k' ? '4K' : 'HD'} Radarr connection successful.`, 'success');
    } catch (error) {
      showMessage(error.message, 'error');
    } finally {
      button.disabled = false;
      button.textContent = originalText;
    }
  });
});

document.querySelectorAll('[data-clear]').forEach((button) => {
  button.addEventListener('click', async () => {
    const quality = button.dataset.clear;
    if (!window.confirm(`Clear the ${quality === '4k' ? '4K' : 'HD'} Radarr connection settings?`)) return;
    button.disabled = true;
    try {
      await request(`/api/settings/radarr/${quality}`, { method: 'DELETE' });
      instances[quality].apiKey.value = '';
      await loadSettings();
      showMessage('Radarr connection cleared.', 'success');
    } catch (error) {
      showMessage(error.message, 'error');
    } finally {
      button.disabled = false;
    }
  });
});

loadSettings().catch((error) => showMessage(error.message, 'error'));