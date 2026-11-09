async function request(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options.headers },
  });
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || 'The request could not be completed.');
  return data;
}

// Wires up one settings form (Radarr or Sonarr). Both have a 4K and an HD
// instance and talk to /api/settings/<prefix>.
function initSettingsForm(form) {
  const prefix = form.dataset.prefix;
  const name = form.dataset.name;
  const api = `/api/settings/${prefix}`;
  const message = document.querySelector(`#${prefix}-message`);
  const saveButton = form.querySelector('button[type="submit"]');

  const instances = {
    '4k': {
      key: 'fourK',
      url: document.querySelector(`#${prefix}-4k-url`),
      apiKey: document.querySelector(`#${prefix}-4k-key`),
      state: document.querySelector(`#${prefix}-4k-state`),
      folder: document.querySelector(`#${prefix}-4k-folder`),
      exclude: document.querySelector(`#${prefix}-4k-exclude`),
    },
    hd: {
      key: 'hd',
      url: document.querySelector(`#${prefix}-hd-url`),
      apiKey: document.querySelector(`#${prefix}-hd-key`),
      state: document.querySelector(`#${prefix}-hd-state`),
      folder: document.querySelector(`#${prefix}-hd-folder`),
      exclude: document.querySelector(`#${prefix}-hd-exclude`),
    },
  };
  const label = (quality) => `${quality === '4k' ? '4K' : 'HD'} ${name}`;

  function showMessage(text, kind = '') {
    message.textContent = text;
    message.className = `settings-message ${kind}`.trim();
  }

  function formatSize(bytes) {
    if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit += 1;
    }
    return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
  }

  // The header reports free space for one root folder. List the connected
  // instance's folders so the user can pick which; blank means automatic.
  async function fillFolders(quality, item, saved) {
    const select = item.folder;
    select.replaceChildren(new Option('Automatic (folder holding the most media)', ''));
    if (!saved.configured) {
      select.options[0].textContent = 'Save the connection to choose a folder';
      select.disabled = true;
      return;
    }
    select.disabled = false;
    let paths = [];
    try {
      const { folders } = await request(`${api}/rootfolders?quality=${quality}`);
      paths = folders.map((folder) => folder.path);
      for (const folder of folders) {
        select.append(new Option(`${folder.path} (${formatSize(folder.freeSpace)} free)`, folder.path));
      }
    } catch {
      // Instance unreachable: fall through and keep the saved choice selectable.
    }
    // Never drop a saved folder just because the list could not be loaded.
    if (saved.rootFolder && !paths.includes(saved.rootFolder)) {
      select.append(new Option(`${saved.rootFolder} (saved)`, saved.rootFolder));
    }
    select.value = saved.rootFolder || '';
  }

  async function loadSettings() {
    const data = await request(api);
    for (const [quality, item] of Object.entries(instances)) {
      const saved = data[item.key];
      item.url.value = saved.url || '';
      item.state.textContent = saved.configured ? 'Configured' : 'Not configured';
      item.state.classList.toggle('configured', saved.configured);
      item.exclude.checked = Boolean(saved.excludeOnDelete);
      form.querySelector(`[data-clear="${quality}"]`).hidden = !saved.configured;
      await fillFolders(quality, item, saved);
    }
  }

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    saveButton.disabled = true;
    showMessage('Saving settings…');
    try {
      await request(api, {
        method: 'PUT',
        body: JSON.stringify({
          fourK: { url: instances['4k'].url.value, apiKey: instances['4k'].apiKey.value, rootFolder: instances['4k'].folder.value, excludeOnDelete: instances['4k'].exclude.checked },
          hd: { url: instances.hd.url.value, apiKey: instances.hd.apiKey.value, rootFolder: instances.hd.folder.value, excludeOnDelete: instances.hd.exclude.checked },
        }),
      });
      for (const instance of Object.values(instances)) instance.apiKey.value = '';
      await loadSettings();
      showMessage(`${name} settings saved.`, 'success');
    } catch (error) {
      showMessage(error.message, 'error');
    } finally {
      saveButton.disabled = false;
    }
  });

  form.querySelectorAll('[data-test]').forEach((button) => {
    button.addEventListener('click', async () => {
      const quality = button.dataset.test;
      const instance = instances[quality];
      const originalText = button.textContent;
      button.disabled = true;
      button.textContent = 'Testing…';
      showMessage(`Testing ${label(quality)}…`);
      try {
        await request(`${api}/test`, {
          method: 'POST',
          body: JSON.stringify({
            quality,
            instance: { url: instance.url.value, apiKey: instance.apiKey.value },
          }),
        });
        showMessage(`${label(quality)} connection successful.`, 'success');
      } catch (error) {
        showMessage(error.message, 'error');
      } finally {
        button.disabled = false;
        button.textContent = originalText;
      }
    });
  });

  form.querySelectorAll('[data-clear]').forEach((button) => {
    button.addEventListener('click', async () => {
      const quality = button.dataset.clear;
      if (!window.confirm(`Clear the ${label(quality)} connection settings?`)) return;
      button.disabled = true;
      try {
        await request(`${api}/${quality}`, { method: 'DELETE' });
        instances[quality].apiKey.value = '';
        await loadSettings();
        showMessage(`${label(quality)} connection cleared.`, 'success');
      } catch (error) {
        showMessage(error.message, 'error');
      } finally {
        button.disabled = false;
      }
    });
  });

  loadSettings().catch((error) => showMessage(error.message, 'error'));
}

document.querySelectorAll('.arr-settings-form').forEach(initSettingsForm);

// Lidarr is a single connection (no 4K/HD pair), so it has its own small form.
function initLidarrForm(form) {
  const message = document.querySelector('#lidarr-message');
  const url = document.querySelector('#lidarr-url');
  const apiKey = document.querySelector('#lidarr-key');
  const state = document.querySelector('#lidarr-state');
  const testButton = document.querySelector('#lidarr-test');
  const clearButton = document.querySelector('#lidarr-clear');
  const saveButton = document.querySelector('#save-lidarr-settings');

  function showMessage(text, kind = '') {
    message.textContent = text;
    message.className = `settings-message ${kind}`.trim();
  }

  const folder = document.querySelector('#lidarr-folder');

  function formatSize(bytes) {
    if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit += 1;
    }
    return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
  }

  // The header reports free space for one root folder; blank means automatic.
  async function fillFolders(saved) {
    folder.replaceChildren(new Option('Automatic (folder holding the most music)', ''));
    if (!saved.configured) {
      folder.options[0].textContent = 'Save the connection to choose a folder';
      folder.disabled = true;
      return;
    }
    folder.disabled = false;
    let paths = [];
    try {
      const { folders } = await request('/api/settings/lidarr/rootfolders');
      paths = folders.map((item) => item.path);
      for (const item of folders) {
        folder.append(new Option(`${item.path} (${formatSize(item.freeSpace)} free)`, item.path));
      }
    } catch {
      // Lidarr unreachable: keep the saved choice selectable below.
    }
    if (saved.rootFolder && !paths.includes(saved.rootFolder)) {
      folder.append(new Option(`${saved.rootFolder} (saved)`, saved.rootFolder));
    }
    folder.value = saved.rootFolder || '';
  }

  async function load() {
    const saved = await request('/api/settings/lidarr');
    url.value = saved.url || '';
    state.textContent = saved.configured ? 'Configured' : 'Not configured';
    state.classList.toggle('configured', saved.configured);
    clearButton.hidden = !saved.configured;
    await fillFolders(saved);
  }

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    saveButton.disabled = true;
    showMessage('Saving settings…');
    try {
      await request('/api/settings/lidarr', {
        method: 'PUT',
        body: JSON.stringify({ url: url.value, apiKey: apiKey.value, rootFolder: folder.value }),
      });
      apiKey.value = '';
      await load();
      showMessage('Lidarr settings saved.', 'success');
    } catch (error) {
      showMessage(error.message, 'error');
    } finally {
      saveButton.disabled = false;
    }
  });

  testButton.addEventListener('click', async () => {
    const originalText = testButton.textContent;
    testButton.disabled = true;
    testButton.textContent = 'Testing…';
    showMessage('Testing Lidarr…');
    try {
      await request('/api/settings/lidarr/test', {
        method: 'POST',
        body: JSON.stringify({ instance: { url: url.value, apiKey: apiKey.value } }),
      });
      showMessage('Lidarr connection successful.', 'success');
    } catch (error) {
      showMessage(error.message, 'error');
    } finally {
      testButton.disabled = false;
      testButton.textContent = originalText;
    }
  });

  clearButton.addEventListener('click', async () => {
    if (!window.confirm('Clear the Lidarr connection settings?')) return;
    clearButton.disabled = true;
    try {
      await request('/api/settings/lidarr', { method: 'DELETE' });
      apiKey.value = '';
      await load();
      showMessage('Lidarr connection cleared.', 'success');
    } catch (error) {
      showMessage(error.message, 'error');
    } finally {
      clearButton.disabled = false;
    }
  });

  load().catch((error) => showMessage(error.message, 'error'));
}

const lidarrForm = document.querySelector('#lidarr-settings-form');
if (lidarrForm) initLidarrForm(lidarrForm);

// Extra ratings providers (OMDb, Trakt): a key each. Saved keys are never sent
// back to the page, only whether one is set.
function initRatingsForm(form) {
  const message = document.querySelector('#ratings-message');
  const saveButton = document.querySelector('#save-ratings-settings');
  const providers = {
    omdb: { input: document.querySelector('#ratings-omdb-key'), state: document.querySelector('#ratings-omdb-state'), name: 'OMDb' },
    trakt: { input: document.querySelector('#ratings-trakt-key'), state: document.querySelector('#ratings-trakt-state'), name: 'Trakt' },
  };

  function showMessage(text, kind = '') {
    message.textContent = text;
    message.className = `settings-message ${kind}`.trim();
  }

  async function load() {
    const configured = await request('/api/settings/ratings');
    for (const [id, provider] of Object.entries(providers)) {
      provider.state.textContent = configured[id] ? 'Configured' : 'Not configured';
      provider.state.classList.toggle('configured', configured[id]);
      form.querySelector(`[data-ratings-clear="${id}"]`).hidden = !configured[id];
    }
  }

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    saveButton.disabled = true;
    showMessage('Saving…');
    try {
      await request('/api/settings/ratings', {
        method: 'PUT',
        body: JSON.stringify({ omdbKey: providers.omdb.input.value, traktClientId: providers.trakt.input.value }),
      });
      for (const provider of Object.values(providers)) provider.input.value = '';
      await load();
      showMessage('Ratings settings saved. New lookups start after your next scan.', 'success');
    } catch (error) {
      showMessage(error.message, 'error');
    } finally {
      saveButton.disabled = false;
    }
  });

  form.querySelectorAll('[data-ratings-clear]').forEach((button) => {
    button.addEventListener('click', async () => {
      const id = button.dataset.ratingsClear;
      if (!window.confirm(`Remove the saved ${providers[id].name} key?`)) return;
      button.disabled = true;
      try {
        await request(`/api/settings/ratings/${id}`, { method: 'DELETE' });
        await load();
        showMessage(`${providers[id].name} key removed.`, 'success');
      } catch (error) {
        showMessage(error.message, 'error');
      } finally {
        button.disabled = false;
      }
    });
  });

  load().catch((error) => showMessage(error.message, 'error'));
}

const ratingsForm = document.querySelector('#ratings-settings-form');
if (ratingsForm) initRatingsForm(ratingsForm);
