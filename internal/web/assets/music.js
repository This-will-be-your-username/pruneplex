// The Music page: Lidarr artists, each collapsible to show its albums.
// "Delete tracks" on an album stops monitoring it and deletes its track files;
// the album itself stays in Lidarr.

const list = document.querySelector('#music-list');
const message = document.querySelector('#music-message');
const summary = document.querySelector('#music-summary');
const tools = document.querySelector('#music-tools');
const searchInput = document.querySelector('#music-search');
const showEmptyBox = document.querySelector('#music-show-empty');
const note = document.querySelector('#music-note');
const refreshButton = document.querySelector('#music-refresh');

let library = null;
const openArtists = new Set();

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
  message.textContent = text;
  message.className = `plex-message ${kind}`.trim();
}

function formatFileSize(bytes) {
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

function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function coverImage(path, className, alt) {
  const wrap = el('span', `${className} cover-wrap`);
  wrap.append(el('span', 'cover-fallback'));
  if (path) {
    const img = document.createElement('img');
    img.alt = alt;
    img.loading = 'lazy';
    img.src = `/api/lidarr/cover?path=${encodeURIComponent(path)}`;
    img.addEventListener('error', () => img.remove());
    wrap.append(img);
  }
  return wrap;
}

async function load() {
  refreshButton.disabled = true;
  showMessage('Loading your Lidarr library…');
  try {
    library = await request('/api/lidarr/library');
    showMessage('');
    tools.hidden = false;
    render();
  } catch (error) {
    library = null;
    list.replaceChildren();
    tools.hidden = true;
    summary.replaceChildren();
    showMessage(error.message, 'error');
    if (/settings page/i.test(error.message)) {
      const link = el('a', 'configure-link', ' Open Settings');
      link.href = '/settings';
      message.append(link);
    }
  } finally {
    refreshButton.disabled = false;
  }
}

function matchesQuery(text, query) {
  return text.toLowerCase().includes(query);
}

/** Which artists and albums to show, given the search text and the empty-album toggle. */
function visibleArtists() {
  const query = (searchInput.value || '').trim().toLowerCase();
  const showEmpty = showEmptyBox.checked;
  const out = [];
  for (const artist of library.artists) {
    const artistMatches = !query || matchesQuery(artist.name, query);
    let albums = artist.albums.filter((album) => showEmpty || album.trackFiles > 0);
    // When only album titles match, narrow the albums to those.
    if (query && !artistMatches) albums = albums.filter((album) => matchesQuery(album.title, query));
    if (!artistMatches && albums.length === 0) continue; // nothing here matches
    if (albums.length === 0 && !showEmpty) continue; // no files to prune
    out.push({ artist, albums, forceOpen: Boolean(query) && !artistMatches });
  }
  return out;
}

function render() {
  if (!library) return;
  const shown = visibleArtists();
  list.replaceChildren();
  for (const { artist, albums, forceOpen } of shown) list.append(artistBlock(artist, albums, forceOpen));

  const albumCount = library.artists.reduce((n, a) => n + a.albums.length, 0);
  summary.replaceChildren();
  const chip = el('span', 'storage-chip');
  chip.append(
    el('b', '', 'Lidarr'),
    el('span', '', `${library.artists.length} artists · ${albumCount} albums · ${formatFileSize(library.usedBytes)} used${library.storage ? ` · ${formatFileSize(library.storage.freeBytes)} free` : ''}`),
  );
  if (library.storage?.folder) chip.title = `Free space of ${library.storage.folder}`;
  summary.append(chip);

  const query = (searchInput.value || '').trim();
  note.textContent = shown.length === 0
    ? (query ? 'No matches' : 'Nothing to show. Try “Show albums without files”.')
    : `${shown.length} artist${shown.length === 1 ? '' : 's'}`;
}

function artistBlock(artist, albums, forceOpen) {
  const details = el('details', 'artist');
  details.open = forceOpen || openArtists.has(artist.id);
  details.addEventListener('toggle', () => {
    if (details.open) openArtists.add(artist.id); else openArtists.delete(artist.id);
  });

  const head = el('summary', 'artist-summary');
  head.append(coverImage(artist.cover, 'artist-cover', ''));
  const names = el('span', 'artist-names');
  names.append(el('span', 'artist-name', artist.name));
  const withFiles = artist.albums.filter((album) => album.trackFiles > 0).length;
  names.append(el('span', 'artist-meta',
    `${withFiles} album${withFiles === 1 ? '' : 's'} with files · ${artist.trackFiles} tracks · ${formatFileSize(artist.sizeBytes)}`));
  head.append(names);
  if (artist.status) head.append(el('span', `chip chip-${artist.status}`, artist.status));
  if (!artist.monitored) head.append(el('span', 'chip chip-muted', 'not monitored'));
  const deleteArtist = el('button', 'button button-danger button-sm artist-delete', 'Delete artist');
  deleteArtist.type = 'button';
  deleteArtist.title = 'Remove this artist from Lidarr and delete all of its files';
  deleteArtist.addEventListener('click', (event) => {
    // The button lives inside <summary>; don't let the click toggle the artist open or closed.
    event.preventDefault();
    event.stopPropagation();
    removeArtist(artist, deleteArtist);
  });
  head.append(deleteArtist);
  details.append(head);

  const items = el('ul', 'album-list');
  for (const album of albums) items.append(albumRow(artist, album));
  details.append(items);
  return details;
}

function albumRow(artist, album) {
  const row = el('li', 'album-row');
  if (album.trackFiles === 0) row.classList.add('is-empty');
  row.append(coverImage(album.cover, 'album-cover', ''));

  const info = el('div', 'album-info');
  info.append(el('span', 'album-title', album.title));
  const meta = [album.type, album.year || ''].filter(Boolean).join(' · ');
  if (meta) info.append(el('span', 'album-meta', meta));
  row.append(info);

  row.append(el('span', 'album-tracks', `${album.trackFiles}/${album.tracks} tracks`));
  row.append(el('span', 'album-size', formatFileSize(album.sizeBytes)));
  row.append(el('span', `chip ${album.monitored ? 'chip-continuing' : 'chip-muted'}`, album.monitored ? 'monitored' : 'not monitored'));

  const button = el('button', 'button button-danger button-sm album-delete', album.trackFiles > 0 ? 'Delete tracks' : 'No files');
  button.type = 'button';
  button.disabled = album.trackFiles === 0;
  button.addEventListener('click', () => removeAlbum(artist, album, button));
  row.append(button);
  return row;
}

async function removeAlbum(artist, album, button) {
  const count = album.trackFiles;
  const ok = window.confirm(
    `Delete the ${count} track file${count === 1 ? '' : 's'} of “${album.title}” by ${artist.name} from disk and stop monitoring the album?\n\n` +
    'The album stays in Lidarr. This cannot be undone here.',
  );
  if (!ok) return;
  button.disabled = true;
  button.textContent = 'Deleting…';
  try {
    const result = await request('/api/lidarr/album/remove', {
      method: 'POST',
      body: JSON.stringify({ albumId: album.id }),
    });
    artist.trackFiles = Math.max(0, artist.trackFiles - (result.removedFiles || 0));
    artist.sizeBytes = Math.max(0, artist.sizeBytes - (result.freedBytes || 0));
    library.usedBytes = Math.max(0, library.usedBytes - (result.freedBytes || 0));
    album.trackFiles = 0;
    album.sizeBytes = 0;
    album.monitored = false;
    showMessage(`Deleted ${result.removedFiles} track file${result.removedFiles === 1 ? '' : 's'} of “${album.title}” and stopped monitoring it.`, 'success');
    render();
  } catch (error) {
    // If Lidarr already stopped monitoring the album, the message says so; use
    // Refresh to pick up its current state.
    showMessage(error.message, 'error');
    button.disabled = false;
    button.textContent = 'Delete tracks';
  }
}

async function removeArtist(artist, button) {
  const files = artist.trackFiles;
  const ok = window.confirm(
    `Remove “${artist.name}” from Lidarr entirely?\n\n` +
    `This deletes the artist and all ${artist.albums.length} of their albums from Lidarr` +
    (files > 0 ? ` and deletes ${files} track file${files === 1 ? '' : 's'} (${formatFileSize(artist.sizeBytes)}) from disk` : '') +
    '.\n\nThis cannot be undone here.',
  );
  if (!ok) return;
  button.disabled = true;
  button.textContent = 'Deleting…';
  try {
    await request('/api/lidarr/artist/remove', {
      method: 'POST',
      body: JSON.stringify({ artistId: artist.id }),
    });
    library.artists = library.artists.filter((item) => item.id !== artist.id);
    library.usedBytes = Math.max(0, library.usedBytes - artist.sizeBytes);
    // Approximate until the next Refresh: the freed space is now available.
    if (library.storage) library.storage.freeBytes += artist.sizeBytes;
    openArtists.delete(artist.id);
    showMessage(`Removed “${artist.name}” from Lidarr${files > 0 ? ` and deleted ${formatFileSize(artist.sizeBytes)} of files` : ''}.`, 'success');
    render();
  } catch (error) {
    showMessage(error.message, 'error');
    button.disabled = false;
    button.textContent = 'Delete artist';
  }
}

searchInput.addEventListener('input', render);
searchInput.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && searchInput.value) {
    searchInput.value = '';
    render();
  }
});
showEmptyBox.addEventListener('change', render);
refreshButton.addEventListener('click', load);
document.querySelector('#music-expand-all').addEventListener('click', () => {
  for (const { artist } of visibleArtists()) openArtists.add(artist.id);
  render();
});
document.querySelector('#music-collapse-all').addEventListener('click', () => {
  openArtists.clear();
  render();
});

load();
