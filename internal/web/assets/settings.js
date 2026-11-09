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
