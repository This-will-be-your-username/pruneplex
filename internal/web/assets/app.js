const doc = typeof document !== 'undefined' ? document : null;
const connectButton = doc?.querySelector('#plex-connect');
const disconnectButton = doc?.querySelector('#plex-disconnect');
const analyzeButton = doc?.querySelector('#plex-analyze');
const librarySelect = doc?.querySelector('#plex-library-select');
const message = doc?.querySelector('#plex-message');
const serverPicker = doc?.querySelector('#plex-server-picker');
const serverSelect = doc?.querySelector('#plex-server-select');
const serverConnectButton = doc?.querySelector('#plex-server-connect');
const results = doc?.querySelector('#plex-results');
const resultCount = doc?.querySelector('#plex-result-count');
const resultsBody = doc?.querySelector('#plex-results-body');
const emptyResults = doc?.querySelector('#plex-empty-results');
const highlightInfo = doc?.querySelector('#plex-highlight-info');
const filterContainer = doc?.querySelector('#filter-container');
const filterSelect = doc?.querySelector('#filter-select');
const applyFilterBtn = doc?.querySelector('#apply-filter-btn');
const clearFilterBtn = doc?.querySelector('#clear-filter-btn');

let currentAnalysis = null;

// The page heading names the Plex library being analyzed. Until an analysis has
// run it follows the library dropdown; afterwards it stays on the library that
// was analyzed, so it always matches the results below it.
const heading = doc?.querySelector('#status-title');
const defaultHeading = heading?.textContent ?? '';
let analyzedLibrary = null;

function selectedLibraryName() {
  if (!librarySelect || librarySelect.hidden) return null;
  return librarySelect.selectedOptions?.[0]?.textContent || null;
}

function updateHeading() {
  if (heading) heading.textContent = analyzedLibrary ?? selectedLibraryName() ?? defaultHeading;
}

// The Movies page (Radarr) and the TV Shows page (Sonarr) share this script;
// <body data-media> says which one we are on.
const isTV = doc?.body?.dataset?.media === 'tv';
const MEDIA = isTV
  ? {
      noun: 'shows',
      arr: 'Sonarr',
      libraryType: 'show',
      allLibraries: 'All TV libraries',
      libraryKey: 'pruneplex.library.tv',
      analyzeURL: '/api/plex/analyze-tv',
      restoreURL: '/api/plex/analysis-tv',
      analyzedKey: 'pruneplex.analyzed.tv',
      storageURL: '/api/sonarr/storage',
      removeURL: '/api/sonarr/series/remove',
      removeBody: (quality, id) => ({ quality, seriesId: id }),
      removeWhat: 'series and all of its episode files',
      matching: true,
      candidatesURL: '/api/sonarr/candidates',
      matchURL: '/api/sonarr/match',
      clearURL: '/api/sonarr/match/clear',
      scanning: 'Scanning TV libraries…',
      done: 'TV scan complete.',
    }
  : {
      noun: 'movies',
      arr: 'Radarr',
      libraryType: 'movie',
      allLibraries: 'All movie libraries',
      libraryKey: 'pruneplex.library',
      analyzeURL: '/api/plex/analyze',
      restoreURL: '/api/plex/analysis',
      analyzedKey: 'pruneplex.analyzed.movies',
      storageURL: '/api/radarr/storage',
      removeURL: '/api/radarr/movie/remove',
      removeBody: (quality, id) => ({ quality, movieId: id }),
      removeWhat: 'movie files',
      matching: true,
      candidatesURL: '/api/radarr/candidates',
      matchURL: '/api/radarr/match',
      clearURL: '/api/radarr/match/clear',
      scanning: 'Scanning movie libraries…',
      done: 'Movie scan complete.',
    };

/**
 * The rendering code below speaks in terms of the movie fields (radarr4k,
 * radarrHd, ...). TV analyses carry sonarr* fields, so copy them across once
 * when a response arrives.
 */
function normalizeAnalysis(analysis) {
  if (!isTV) return analysis;
  for (const list of [analysis.matches, analysis.only4k, analysis.onlyHd, analysis.other]) {
    for (const item of list || []) {
      item.radarr4k = item.sonarr4k;
      item.radarrHd = item.sonarrHd;
      item.radarr4kConfigured = item.sonarr4kConfigured;
      item.radarrHdConfigured = item.sonarrHdConfigured;
    }
  }
  return analysis;
}

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
  if (!message) return;
  message.textContent = text;
  message.className = `plex-message ${kind}`.trim();
}

function showServerChoices(servers) {
  if (!serverSelect || !serverPicker) return;
  serverSelect.replaceChildren();
  for (const server of servers) {
    const option = document.createElement('option');
    option.value = server.id;
    option.textContent = server.name;
    serverSelect.append(option);
  }
  serverPicker.hidden = false;
  if (connectButton) connectButton.hidden = true;
  if (disconnectButton) disconnectButton.hidden = true;
  if (analyzeButton) analyzeButton.disabled = true;
  showMessage('Plex account authorized. Choose a server to continue.');
}

const LIBRARY_KEY = MEDIA.libraryKey;

async function loadLibraries() {
  if (!librarySelect) return;
  try {
    const { libraries } = await request(`/api/plex/libraries?type=${MEDIA.libraryType}`);
    librarySelect.replaceChildren();
    if (libraries.length !== 1) {
      const all = document.createElement('option');
      all.value = '';
      all.textContent = MEDIA.allLibraries;
      librarySelect.append(all);
    }
    for (const library of libraries) {
      const option = document.createElement('option');
      option.value = library.key;
      option.textContent = library.title;
      librarySelect.append(option);
    }
    let saved = '';
    try { saved = localStorage.getItem(LIBRARY_KEY) || ''; } catch {}
    if (Array.from(librarySelect.options).some((o) => o.value === saved)) librarySelect.value = saved;
    librarySelect.hidden = false;
    updateHeading();
  } catch (error) {
    showMessage(error.message, 'error');
  }
}

librarySelect?.addEventListener('change', () => {
  try { localStorage.setItem(LIBRARY_KEY, librarySelect.value); } catch {}
  updateHeading();
});

function showConnected(serverName) {
  if (serverPicker) serverPicker.hidden = true;
  if (connectButton) connectButton.hidden = true;
  if (disconnectButton) disconnectButton.hidden = false;
  if (analyzeButton) analyzeButton.disabled = false;
  showMessage(`Connected to ${serverName}.`, 'success');
  loadLibraries();
}

// --- Disk usage as reported by Radarr / Sonarr ---

const storageSummary = doc?.querySelector('#storage-summary');

/**
 * Next to the library name, show for each configured Radarr/Sonarr instance how
 * much space its media uses and how much free space its disks report.
 */
async function loadStorage() {
  if (!storageSummary) return;
  try {
    const data = await request(MEDIA.storageURL);
    storageSummary.replaceChildren();
    for (const [key, label] of [['fourK', '4K'], ['hd', 'HD']]) {
      const info = data[key];
      if (!info?.configured) continue;
      const chip = document.createElement('span');
      chip.className = 'storage-chip';
      if (info.folder) chip.title = `Free space of ${info.folder}`;
      const name = document.createElement('b');
      name.textContent = `${label} ${MEDIA.arr}`;
      const figures = document.createElement('span');
      if (info.error) {
        figures.textContent = 'unavailable';
        chip.classList.add('is-error');
      } else {
        figures.textContent = `${formatFileSize(info.usedBytes)} used · ${formatFileSize(info.freeBytes)} free`;
      }
      chip.append(name, figures);
      storageSummary.append(chip);
    }
  } catch {
    storageSummary.replaceChildren();
  }
}


/**
 * Show the last scan again when the user comes back to this page (the server
 * keeps it for the session). The server re-matches it against Radarr/Sonarr, so
 * deletions and manual matches made since are reflected.
 */
async function restoreAnalysis() {
  try {
    const { analysis } = await request(MEDIA.restoreURL);
    if (!analysis) return;
    try { analyzedLibrary = sessionStorage.getItem(MEDIA.analyzedKey); } catch { analyzedLibrary = null; }
    renderResults(normalizeAnalysis(analysis));
    updateHeading();
    showMessage(`Showing your last scan${analyzedLibrary ? ` of ${analyzedLibrary}` : ''}. Click Analyze to refresh it.`);
  } catch {
    // No saved scan, or Radarr/Sonarr unreachable: start from a clean page.
  }
}
async function refreshStatus() {
  const state = await request('/api/plex/status');
  if (state.connected) {
    showConnected(state.serverName);
    restoreAnalysis();
  } else if (state.servers?.length) {
    showServerChoices(state.servers);
  }
}

connectButton?.addEventListener('click', async () => {
  const authWindow = window.open('about:blank', '_blank');
  if (authWindow) authWindow.opener = null;
  connectButton.disabled = true;
  showMessage('Starting Plex sign-in…');
  try {
    const { authURL } = await request('/api/plex/auth/start', { method: 'POST' });
    if (authWindow) authWindow.location = authURL;
    showMessage('Complete sign-in in the Plex tab. Waiting for approval…');
    let attempts = 0;
    while (attempts < 300) {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      const state = await request('/api/plex/auth/status');
      if (state.state === 'select-server') {
        showServerChoices(state.servers);
        return;
      }
      if (state.state === 'connected') {
        await refreshStatus();
        return;
      }
      if (state.state === 'no-servers') {
        showMessage('No Plex Media Servers were found for this account.', 'error');
        return;
      }
      if (state.state === 'expired') {
        showMessage('The Plex sign-in expired. Start again to get a new code.', 'error');
        return;
      }
      attempts += 1;
    }
    showMessage('Sign-in timed out. Start again to get a new code.', 'error');
  } catch (error) {
    if (authWindow) authWindow.close();
    showMessage(error.message, 'error');
  } finally {
    connectButton.disabled = false;
  }
});

serverConnectButton?.addEventListener('click', async () => {
  serverConnectButton.disabled = true;
  showMessage('Checking server connection…');
  try {
    const state = await request('/api/plex/server', {
      method: 'POST',
      body: JSON.stringify({ id: serverSelect.value }),
    });
    showConnected(state.serverName);
  } catch (error) {
    showMessage(error.message, 'error');
  } finally {
    serverConnectButton.disabled = false;
  }
});

disconnectButton?.addEventListener('click', async () => {
  await request('/api/plex/disconnect', { method: 'POST' });
  if (connectButton) connectButton.hidden = false;
  if (disconnectButton) disconnectButton.hidden = true;
  if (analyzeButton) analyzeButton.disabled = true;
  if (librarySelect) librarySelect.hidden = true;
  analyzedLibrary = null;
  try { sessionStorage.removeItem(MEDIA.analyzedKey); } catch {}
  updateHeading();
  if (serverPicker) serverPicker.hidden = true;
  if (results) results.hidden = true;
  if (highlightInfo) highlightInfo.hidden = true;
  if (filterContainer) filterContainer.hidden = true;
  currentAnalysis = null;
  showMessage('Plex disconnected.');
});

analyzeButton?.addEventListener('click', async () => {
  analyzeButton.disabled = true;
  analyzeButton.classList.add('loading');
  showMessage(MEDIA.scanning);
  try {
    const analysis = await request(MEDIA.analyzeURL, {
      method: 'POST',
      body: JSON.stringify({ library: librarySelect?.value ?? '' }),
    });
    analyzedLibrary = selectedLibraryName();
    try { if (analyzedLibrary) sessionStorage.setItem(MEDIA.analyzedKey, analyzedLibrary); } catch {}
    renderResults(normalizeAnalysis(analysis));
    updateHeading();
    showMessage(MEDIA.done, 'success');
  } catch (error) {
    showMessage(error.message, 'error');
  } finally {
    analyzeButton.disabled = false;
    analyzeButton.classList.remove('loading');
  }
});

// --- Filter UI ---

applyFilterBtn?.addEventListener('click', () => {
  const val = filterSelect ? filterSelect.value : '';
  if (val) applyFilter(val);
});

clearFilterBtn?.addEventListener('click', () => {
  clearFilter();
});

function clearFilter() {
  const rows = document.querySelectorAll('#plex-results-body tr');
  rows.forEach(r => r.hidden = false);
  if (filterSelect) filterSelect.value = '';
}

function applyFilter(filter) {
  const rows = document.querySelectorAll('#plex-results-body tr');
  const movies = currentAnalysis ? currentAnalysis.matches : [];

  // Hide all rows first
  rows.forEach(r => r.hidden = true);

  let matchIndices = [];
  switch (filter) {
    case 'top20_4k': {
      const entries = movies.map((m, i) => ({ size: get4KSize(m), i })).filter(o => o.size > 0);
      const threshold = percentileThreshold(entries.map(o => o.size), 0.20, false);
      matchIndices = entries.filter(o => o.size >= threshold).map(o => o.i);
      break;
    }
    case 'top20_hd': {
      const entries = movies.map((m, i) => ({ size: getHDSize(m), i })).filter(o => o.size > 0);
      const threshold = percentileThreshold(entries.map(o => o.size), 0.20, false);
      matchIndices = entries.filter(o => o.size >= threshold).map(o => o.i);
      break;
    }
    case 'lowest_20_rated': {
      const entries = movies.map((m, i) => ({ score: compositeRating(m), i })).filter(o => o.score > 0);
      const threshold = percentileThreshold(entries.map(o => o.score), 0.20, true);
      matchIndices = entries.filter(o => o.score <= threshold).map(o => o.i);
      break;
    }
    case 'hd_gt_fourk': {
      matchIndices = movies
        .map((m, i) => ({ i, fourK: get4KSize(m), hd: getHDSize(m) }))
        .filter(o => o.fourK > 0 && o.hd > 0 && o.hd > o.fourK)
        .map(o => o.i);
      break;
    }
    case 'fourk_within_10_hd': {
      matchIndices = movies
        .map((m, i) => ({ i, fourK: get4KSize(m), hd: getHDSize(m) }))
        .filter(o => o.fourK > 0 && o.hd > 0 && Math.abs(o.fourK - o.hd) <= 0.10 * o.hd)
        .map(o => o.i);
      break;
    }
    default:
      // No filter – show all
      rows.forEach(r => r.hidden = false);
      return;
  }

  // Show matched rows
  for (const i of matchIndices) {
    const row = document.querySelector(`#plex-results-body tr[data-movie-index="${i}"]`);
    if (row) row.hidden = false;
  }
}

/**
 * Compute the threshold value at a given percentile.
 * @param {number[]} values - Array of numeric values.
 * @param {number} percentile - Fraction (e.g. 0.20 for 20%).
 * @param {boolean} lowest - If true, return the threshold for the bottom percentile; otherwise top.
 * @returns {number}
 */
function percentileThreshold(values, percentile, lowest) {
  if (!values.length) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  if (lowest) {
    const index = Math.max(0, Math.ceil(sorted.length * percentile) - 1);
    return sorted[index];
  }
  const index = Math.max(0, sorted.length - Math.ceil(sorted.length * percentile));
  return sorted[index];
}

/**
 * Compute a composite rating (0–100) for a movie by averaging all available ratings.
 */
function compositeRating(movie) {
  const ratings = Array.isArray(movie.ratings) ? movie.ratings : [];
  if (!ratings.length) return 0;
  let total = 0;
  let count = 0;
  for (const r of ratings) {
    if (typeof r.value !== 'number' || r.value <= 0) continue;
    let val = r.value;
    // Normalize to 0–100 scale
    if (val <= 10) val = Math.round(val * 10);
    total += val;
    count++;
  }
  return count ? Math.round(total / count) : 0;
}

// --- Render results ---

function renderResults(analysis) {
  currentAnalysis = analysis;
  if (!resultsBody) return;
  resultsBody.replaceChildren();
  if (resultCount) {
    resultCount.textContent = `${analysis.matches.length} matches / ${analysis.scanned.toLocaleString()} ${MEDIA.noun} scanned`;
  }
  if (emptyResults) {
    emptyResults.hidden = analysis.matches.length !== 0;
  }
  const tableScroll = doc?.querySelector('#panel-multi .table-scroll');
  if (tableScroll) {
    tableScroll.hidden = analysis.matches.length === 0;
  }
  for (const [index, movie] of analysis.matches.entries()) {
    resultsBody.append(buildRow(movie, index, ['4k', 'hd']));
  }
  renderSingleResolution('only4k', analysis.only4k || [], ['4k']);
  renderSingleResolution('onlyhd', analysis.onlyHd || [], ['hd']);
  renderSingleResolution('other', analysis.other || [], ['4k', 'hd']);
  setText('#count-multi', analysis.matches.length);
  if (results) results.hidden = false;
  if (filterContainer) filterContainer.hidden = false;
}

function setText(selector, value) {
  const el = doc?.querySelector(selector);
  if (el) el.textContent = String(value);
}

function renderSingleResolution(prefix, movies, columns) {
  const body = doc?.querySelector(`#${prefix}-body`);
  if (!body) return;
  body.replaceChildren();
  for (const [index, movie] of movies.entries()) {
    body.append(buildRow(movie, index, columns));
  }
  setText(`#${prefix}-result-count`, `${movies.length} movies`);
  setText(`#count-${prefix}`, movies.length);
  const empty = doc.querySelector(`#${prefix}-empty`);
  if (empty) empty.hidden = movies.length !== 0;
  const table = doc.querySelector(`#${prefix}-table`);
  if (table) table.hidden = movies.length === 0;
}

function buildRow(movie, index, qualities) {
    const row = document.createElement('tr');
    const fourKSize = get4KSize(movie);
    if (fourKSize > 0) {
      row.dataset.fourKSize = String(fourKSize);
    }
    const hdSize = getHDSize(movie);
    if (hdSize > 0) {
      row.dataset.hdSize = String(hdSize);
    }
    row.dataset.movieIndex = String(index);

    const titleCell = document.createElement('th');
    titleCell.scope = 'row';
    titleCell.className = 'movie-title-cell';

    const movieItem = document.createElement('div');
    movieItem.className = 'movie-item';

    const posterWrap = document.createElement('div');
    posterWrap.className = 'movie-poster-wrap';

    const posterFallback = document.createElement('div');
    posterFallback.className = 'movie-poster-fallback';
    posterFallback.setAttribute('aria-hidden', 'true');
    posterFallback.innerHTML = '<span class="poster-icon"></span>';

    if (movie.thumb) {
      const posterImg = document.createElement('img');
      posterImg.className = 'movie-poster';
      posterImg.src = `/api/plex/thumb?path=${encodeURIComponent(movie.thumb)}`;
      posterImg.alt = movie.title;
      posterImg.loading = 'lazy';
      posterFallback.style.display = 'none';
      posterImg.addEventListener('error', () => {
        posterImg.remove();
        posterFallback.style.display = 'flex';
      });
      posterWrap.append(posterImg);
    }
    posterWrap.append(posterFallback);

    const movieMeta = document.createElement('div');
    movieMeta.className = 'movie-meta';

    const titleSpan = document.createElement('span');
    titleSpan.className = 'movie-title';
    titleSpan.textContent = movie.title;
    movieMeta.append(titleSpan);

    if (movie.year) {
      const yearSpan = document.createElement('span');
      yearSpan.className = 'movie-year';
      yearSpan.textContent = movie.year;
      movieMeta.append(yearSpan);
    }

    movieItem.append(posterWrap, movieMeta);
    if (isTV) movieItem.prepend(createStatusIcon(movie)); // to the left of the poster
    titleCell.append(movieItem);
    const ratingsCell = createRatingsCell(movie);
    const resolutionCell = document.createElement('td');
    resolutionCell.className = 'resolution-cell';
    const resolutionList = document.createElement('div');
    resolutionList.className = 'resolution-list';
    for (const resolution of movie.resolutions) {
      const tag = document.createElement('span');
      tag.className = `resolution-tag${is4K(resolution) ? ' is-4k' : ''}`;
      const episodes = movie.resolutionEpisodes?.[resolution];
      tag.textContent = episodes ? `${resolution} · ${episodes} ep${episodes === 1 ? '' : 's'}` : resolution;
      resolutionList.append(tag);
    }
    resolutionCell.append(resolutionList);
    row.append(titleCell, ratingsCell, resolutionCell);
    row.dataset.columns = qualities.join(',');
    for (const quality of qualities) row.append(createRadarrCell(movie, quality, qualities));
    return row;
}

// --- Tabs ---

function activateTab(name) {
  for (const tab of doc?.querySelectorAll('.tab') ?? []) {
    const active = tab.dataset.tab === name;
    tab.classList.toggle('active', active);
    tab.setAttribute('aria-selected', String(active));
    tab.tabIndex = active ? 0 : -1;
  }
  for (const panel of doc?.querySelectorAll('.tab-panel') ?? []) {
    panel.hidden = panel.id !== `panel-${name}`;
  }
}

doc?.querySelector('.tabs')?.addEventListener('click', (event) => {
  const tab = event.target.closest('.tab');
  if (tab) activateTab(tab.dataset.tab);
});

doc?.querySelector('.tabs')?.addEventListener('keydown', (event) => {
  if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
  const tabs = Array.from(doc.querySelectorAll('.tab'));
  const current = tabs.findIndex((t) => t.classList.contains('active'));
  const next = tabs[(current + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length];
  activateTab(next.dataset.tab);
  next.focus();
});

// --- Ratings UI ---

function createRatingsCell(movie) {
  const cell = document.createElement('td');
  cell.className = 'ratings-cell';

  const ratings = Array.isArray(movie.ratings) ? movie.ratings : [];
  if (!ratings.length) {
    const emptySpan = document.createElement('span');
    emptySpan.className = 'ratings-empty';
    emptySpan.textContent = '—';
    cell.append(emptySpan);
    return cell;
  }

  const list = document.createElement('div');
  list.className = 'ratings-list';

  for (const rating of ratings) {
    const badge = createRatingBadge(rating);
    if (badge) {
      list.append(badge);
    }
  }

  if (!list.children.length) {
    const emptySpan = document.createElement('span');
    emptySpan.className = 'ratings-empty';
    emptySpan.textContent = '—';
    cell.append(emptySpan);
    return cell;
  }

  cell.append(list);
  return cell;
}

function createRatingBadge(rating) {
  if (!rating || typeof rating.value !== 'number' || rating.value <= 0) {
    return null;
  }

  const badge = document.createElement('span');
  badge.className = 'rating-badge';

  const source = (rating.source || '').toLowerCase();
  const image = (rating.image || '').toLowerCase();
  const val = rating.value;

  switch (source) {
    case 'rotten_tomatoes': {
      const score = val <= 10 ? Math.round(val * 10) : Math.round(val);
      const isCertified = image.includes('certified');
      const isRotten = image.includes('rotten') || score < 60;
      badge.classList.add('rating-rt', isRotten ? 'is-rotten' : 'is-fresh');
      badge.title = `Rotten Tomatoes Critic: ${score}%${isCertified ? ' (Certified Fresh)' : ''}`;

      const iconSvg = isRotten
        ? `<svg class="rating-icon rt-icon" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path fill="#6ba539" d="M8 2c-1.5 2-4 1.5-5 3.5S1 9 3 11s3.5 3 6 3 5-1.5 5.5-4-1-3.5-2-4.5-2-2-4.5-3.5z"/></svg>`
        : `<svg class="rating-icon rt-icon" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path fill="#fa320a" d="M8 3c-4 0-6 2.5-6 6.5C2 13 4.5 15 8 15s6-2 6-5.5C14 5.5 12 3 8 3z"/><path fill="#4caf50" d="M8 1c.5 1 1 2 2 2-1 0-1.5.5-2 1-.5-.5-1-1-2-1 1 0 1.5-1 2-2z"/></svg>`;

      badge.innerHTML = `${iconSvg}<span class="rating-score">${score}%</span>`;
      return badge;
    }

    case 'rotten_tomatoes_audience': {
      const score = val <= 10 ? Math.round(val * 10) : Math.round(val);
      const isSpilled = image.includes('spilled') || score < 60;
      badge.classList.add('rating-rt-aud', isSpilled ? 'is-spilled' : 'is-upright');
      badge.title = `Rotten Tomatoes Audience: ${score}%`;

      const iconSvg = isSpilled
        ? `<svg class="rating-icon rt-aud-icon" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path fill="#6ba539" d="M3 8l6-3 4 8-8 3z"/><path fill="#8bc34a" d="M4 6a2 2 0 0 1 3-1 2 2 0 0 1 3 1"/></svg>`
        : `<svg class="rating-icon rt-aud-icon" viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path fill="#f44336" d="M4 6l1 9h6l1-9H4z"/><path fill="#fdd835" d="M5 3a2 2 0 0 1 3-1 2 2 0 0 1 3 1 2 2 0 0 1 2 3H3a2 2 0 0 1 2-3z"/></svg>`;

      badge.innerHTML = `${iconSvg}<span class="rating-score">${score}%</span>`;
      return badge;
    }

    case 'imdb': {
      const score = val <= 10 ? val.toFixed(1) : (val / 10).toFixed(1);
      badge.classList.add('rating-imdb');
      badge.title = `IMDb: ${score}/10`;
      badge.innerHTML = `<span class="rating-logo imdb-logo">IMDb</span><span class="rating-score">${score}</span>`;
      return badge;
    }

    case 'tmdb':
    case 'themoviedb': {
      const score = val <= 10 ? Math.round(val * 10) : Math.round(val);
      badge.classList.add('rating-tmdb');
      badge.title = `TheMovieDB: ${score}%`;
      badge.innerHTML = `<span class="rating-logo tmdb-logo">TMDB</span><span class="rating-score">${score}%</span>`;
      return badge;
    }

    case 'metacritic': {
      const score = Math.round(val <= 10 ? val * 10 : val);
      badge.classList.add('rating-metacritic');
      badge.title = `Metacritic: ${score}/100`;
      badge.innerHTML = `<span class="rating-logo mc-logo">MC</span><span class="rating-score">${score}</span>`;
      return badge;
    }

    case 'user': {
      const score = val.toFixed(val % 1 === 0 ? 0 : 1);
      badge.classList.add('rating-user');
      badge.title = `User Rating: ${score}/10`;
      badge.innerHTML = `<svg class="rating-icon star-icon" viewBox="0 0 16 16" width="11" height="11" fill="#f4c14b" aria-hidden="true"><polygon points="8,1 10,5.5 15,6 11.5,9.5 12.5,14.5 8,12 3.5,14.5 4.5,9.5 1,6 6,5.5"/></svg><span class="rating-score">${score}</span>`;
      return badge;
    }

    default: {
      const score = val <= 10 ? val.toFixed(1) : Math.round(val);
      badge.classList.add('rating-generic');
      const label = rating.type === 'critic' ? 'Critic Score' : (rating.type === 'audience' ? 'Audience Score' : 'Rating');
      badge.title = `${label}: ${score}`;
      badge.innerHTML = `<svg class="rating-icon star-icon" viewBox="0 0 16 16" width="11" height="11" fill="#aaa" aria-hidden="true"><polygon points="8,1 10,5.5 15,6 11.5,9.5 12.5,14.5 8,12 3.5,14.5 4.5,9.5 1,6 6,5.5"/></svg><span class="rating-score">${score}</span>`;
      return badge;
    }
  }
}

// --- Radarr cell ---

function createRadarrCell(movie, quality, columns = ['4k', 'hd']) {
  const cell = document.createElement('td');
  const is4KInstance = quality === '4k';
  if (is4KInstance) {
    cell.classList.add('radarr-cell-4k');
  } else {
    cell.classList.add('radarr-cell-hd');
  }
  const configured = is4KInstance ? movie.radarr4kConfigured : movie.radarrHdConfigured;
  const match = is4KInstance ? movie.radarr4k : movie.radarrHd;
  const label = `${is4KInstance ? '4K' : 'HD'} ${MEDIA.arr}`;
  if (!configured) {
    const link = document.createElement('a');
    link.href = '/settings';
    link.textContent = 'Configure';
    link.className = 'configure-link';
    cell.append(link);
    return cell;
  }
  if (!match) {
    cell.classList.add('radarr-no-match');
    const label = document.createElement('span');
    const wrap = (...children) => {
      const box = document.createElement('div');
      box.className = 'no-match-content';
      box.append(...children);
      return box;
    };
    label.textContent = 'No match';
    // One picker per row fixes every unmatched instance, so only the first
    // unmatched column shows the button (the 4K column when it needs a match).
    const fourKNeedsMatch = columns.includes('4k') && movie.radarr4kConfigured && !movie.radarr4k;
    if (!MEDIA.matching || (!is4KInstance && fourKNeedsMatch)) {
      cell.append(wrap(label));
      return cell;
    }
    const findButton = document.createElement('button');
    findButton.type = 'button';
    findButton.className = 'button button-secondary button-sm find-match-button';
    findButton.textContent = 'Find match';
    const picker = document.createElement('div');
    picker.className = 'match-picker';
    picker.hidden = true;
    findButton.addEventListener('click', () => {
      picker.hidden = !picker.hidden;
      if (!picker.hidden && !picker.dataset.loaded) {
        picker.dataset.loaded = '1';
        buildMatchPicker(picker, movie);
      }
    });
    cell.append(wrap(label, findButton, picker));
    return cell;
  }

  const matchTitle = document.createElement('span');
  matchTitle.className = 'radarr-match-title';
  matchTitle.textContent = match.year ? `${match.title} (${match.year})` : match.title;

  // A match picked by hand (Find match) can be cleared again with a small button.
  const titleRow = document.createElement('div');
  titleRow.className = 'radarr-match-title-row';
  titleRow.append(matchTitle);
  let unlinkButton = null;
  if (match.manual && MEDIA.matching) {
    unlinkButton = document.createElement('button');
    unlinkButton.type = 'button';
    unlinkButton.className = 'unlink-match';
    unlinkButton.textContent = '×';
    unlinkButton.title = 'Clear this manual match';
    unlinkButton.setAttribute('aria-label', `Clear the manual ${label} match for ${movie.title}`);
    unlinkButton.addEventListener('click', async () => {
      unlinkButton.disabled = true;
      try {
        await request(MEDIA.clearURL, {
          method: 'POST',
          body: JSON.stringify({ quality, plexTitle: movie.title, plexYear: movie.year || 0 }),
        });
        if (is4KInstance) movie.radarr4k = null; else movie.radarrHd = null;
        const row = cell.closest('tr');
        cell.replaceWith(createRadarrCell(movie, quality, columns));
        if (row) {
          const size = is4KInstance ? get4KSize(movie) : getHDSize(movie);
          const key = is4KInstance ? 'fourKSize' : 'hdSize';
          if (size > 0) row.dataset[key] = String(size); else delete row.dataset[key];
          if (isTV) row.querySelector('.show-status')?.replaceWith(createStatusIcon(movie));
        }
      } catch (error) {
        unlinkButton.disabled = false;
        showMessage(error.message, 'error');
      }
    });
    titleRow.append(unlinkButton);
  }
  const matchSize = document.createElement('span');
  matchSize.className = 'radarr-match-size';
  if (match.sizeBytes > 0) {
    matchSize.dataset.sizeBytes = String(match.sizeBytes);
  }
  const matchSizeText = document.createElement('span');
  matchSizeText.className = 'radarr-match-size-text';
  matchSizeText.textContent = formatFileSize(match.sizeBytes);
  matchSize.append(matchSizeText);
  const ring = isTV ? createCompletenessRing(match) : null;
  if (ring) matchSize.append(ring);

  const button = document.createElement('button');
  button.className = 'button button-danger radarr-remove-button';
  button.type = 'button';
  button.textContent = `Delete ${is4KInstance ? '4K' : 'HD'}`;
  button.addEventListener('click', async () => {
    if (!window.confirm(`Remove ${match.title} from ${label} and delete its ${MEDIA.removeWhat}? This cannot be undone here.`)) return;
    button.disabled = true;
    button.textContent = 'Removing…';
    try {
      await request(MEDIA.removeURL, {
        method: 'POST',
        body: JSON.stringify(MEDIA.removeBody(quality, match.id)),
      });
      matchTitle.textContent = 'Removed';
      unlinkButton?.remove();
      loadStorage(); // freed space shows up in the header
      ring?.remove();
      button.textContent = 'Removed';
      if (is4KInstance) {
        match.sizeBytes = 0;
        matchSize.dataset.sizeBytes = '0';
        matchSizeText.textContent = 'Removed';
      }
    } catch (error) {
      button.disabled = false;
      button.textContent = `Delete ${is4KInstance ? '4K' : 'HD'}`;
      showMessage(error.message, 'error');
    }
  });
  const wrapper = document.createElement('div');
  wrapper.className = 'radarr-match';
  wrapper.append(titleRow, matchSize, button);
  cell.append(wrapper);
  return cell;
}


// --- Collection completeness ring (TV) ---

/**
 * A small circular gauge with the percentage of a series' monitored, aired
 * episodes that Sonarr has on disk (100% = every episode). Returns null when
 * Sonarr reports no episodes to measure against.
 */
function createCompletenessRing(match) {
  if (!match || !(match.episodeCount > 0)) return null;
  const files = Math.min(match.episodeFiles || 0, match.episodeCount);
  const fraction = files / match.episodeCount;
  // Floor so 99.6% never reads as complete.
  const percent = files === match.episodeCount ? 100 : Math.floor(fraction * 100);
  const radius = 10;
  const circumference = 2 * Math.PI * radius;
  const tone = percent >= 100 ? 'is-complete' : percent >= 50 ? 'is-partial' : 'is-sparse';

  const ring = document.createElement('span');
  ring.className = `completeness-ring ${tone}`;
  ring.title = `${files} of ${match.episodeCount} episodes (${percent}%)`;
  ring.setAttribute('role', 'img');
  ring.setAttribute('aria-label', `${percent}% of episodes present`);
  ring.innerHTML = `<svg viewBox="0 0 26 26" width="26" height="26" aria-hidden="true">
    <circle class="ring-track" cx="13" cy="13" r="${radius}" fill="none" stroke-width="2.5"/>
    <circle class="ring-value" cx="13" cy="13" r="${radius}" fill="none" stroke-width="2.5" stroke-linecap="round"
      stroke-dasharray="${circumference.toFixed(2)}" stroke-dashoffset="${(circumference * (1 - fraction)).toFixed(2)}"
      transform="rotate(-90 13 13)"/>
    <text x="13" y="13" text-anchor="middle" dominant-baseline="central">${percent}</text>
  </svg>`;
  return ring;
}

// --- Series status icon (TV) ---

/**
 * Whether a show is still airing, from the Sonarr series status of whichever
 * instances matched it. "continuing" wins over "ended" if they disagree; no
 * match, or a status such as "upcoming", is unknown.
 */
function seriesStatusOf(movie) {
  const statuses = [movie.radarr4k?.status, movie.radarrHd?.status];
  if (statuses.includes('continuing')) return 'continuing';
  if (statuses.includes('ended')) return 'ended';
  return 'unknown';
}

const STATUS_ICONS = {
  continuing: {
    label: 'Continuing: still airing',
    svg: '<path d="M4 2.5v11l9-5.5z" fill="currentColor"/>',
  },
  ended: {
    label: 'Ended: no new episodes',
    svg: '<rect x="3" y="3" width="10" height="10" rx="1" fill="currentColor"/>',
  },
  unknown: {
    label: 'Status unknown: no Sonarr match',
    svg: '<rect x="3.5" y="2.5" width="3" height="11" rx="0.5" fill="currentColor"/><rect x="9.5" y="2.5" width="3" height="11" rx="0.5" fill="currentColor"/>',
  },
};

function createStatusIcon(movie) {
  const status = seriesStatusOf(movie);
  const icon = STATUS_ICONS[status];
  const el = document.createElement('span');
  el.className = `show-status is-${status}`;
  el.title = icon.label;
  el.setAttribute('role', 'img');
  el.setAttribute('aria-label', icon.label);
  el.innerHTML = `<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">${icon.svg}</svg>`;
  return el;
}
// --- Manual Radarr matching ---

/**
 * Fills `picker` with one shortlist of films that might be `movie`, merged
 * across the 4K and HD Radarr instances, plus a search box to refine it.
 * Choosing one saves the pairing for every instance still lacking a match and
 * re-renders both Radarr cells in the row.
 */
function buildMatchPicker(picker, movie) {
  const form = document.createElement('div');
  form.className = 'match-search';
  const input = document.createElement('input');
  input.type = 'search';
  input.placeholder = `Search ${MEDIA.arr} by title`;
  input.setAttribute('aria-label', `Search ${MEDIA.arr} by title`);
  const searchButton = document.createElement('button');
  searchButton.type = 'button';
  searchButton.className = 'button button-secondary button-sm';
  searchButton.textContent = 'Search';
  form.append(input, searchButton);

  const status = document.createElement('p');
  status.className = 'match-status';
  const list = document.createElement('ul');
  list.className = 'match-candidates';
  picker.append(form, status, list);

  // Instances that are configured but have no match yet.
  // Columns shown in this row (Only 4K / Only HD tabs show just one).
  const rowColumns = () => (picker.closest('tr')?.dataset.columns || '4k,hd').split(',');
  const missing = () => [
    ...(rowColumns().includes('4k') && movie.radarr4kConfigured && !movie.radarr4k ? ['4k'] : []),
    ...(rowColumns().includes('hd') && movie.radarrHdConfigured && !movie.radarrHd ? ['hd'] : []),
  ];

  async function load(query) {
    status.textContent = 'Searching…';
    list.replaceChildren();
    try {
      const { candidates } = await request(MEDIA.candidatesURL, {
        method: 'POST',
        body: JSON.stringify({ title: movie.title, year: movie.year || 0, query }),
      });
      status.textContent = candidates.length
        ? `Pick the matching ${isTV ? 'show' : 'movie'} (applies to 4K and HD):`
        : `No similar ${MEDIA.noun} in ${MEDIA.arr}. Try a different search.`;
      for (const candidate of candidates) {
        const item = document.createElement('li');
        const choose = document.createElement('button');
        choose.type = 'button';
        choose.className = 'match-candidate';
        const title = document.createElement('span');
        title.className = 'match-candidate-title';
        title.textContent = candidate.year ? `${candidate.title} (${candidate.year})` : candidate.title;
        const ids = [candidate.tvdbId ? `TVDB ${candidate.tvdbId}` : '', candidate.tmdbId ? `TMDB ${candidate.tmdbId}` : '', candidate.imdbId || ''].filter(Boolean);
        const detail = document.createElement('span');
        detail.className = 'match-candidate-detail';
        detail.textContent = [
          `4K: ${candidate.fourK ? formatFileSize(candidate.fourK.sizeBytes) : '—'}`,
          `HD: ${candidate.hd ? formatFileSize(candidate.hd.sizeBytes) : '—'}`,
          ...ids,
        ].join(' · ');
        choose.append(title, detail);
        choose.addEventListener('click', () => select(candidate, choose));
        item.append(choose);
        list.append(item);
      }
    } catch (error) {
      status.textContent = error.message;
    }
  }

  async function select(candidate, button) {
    button.disabled = true;
    try {
      const { matches } = await request(MEDIA.matchURL, {
        method: 'POST',
        body: JSON.stringify({
          plexTitle: movie.title,
          plexYear: movie.year || 0,
          title: candidate.title,
          year: candidate.year,
          ...(isTV ? { tvdbId: candidate.tvdbId || 0 } : {}), // the movie endpoint rejects unknown fields
          tmdbId: candidate.tmdbId || 0,
          imdbId: candidate.imdbId || '',
          instances: missing(),
        }),
      });
      if (matches['4k']) movie.radarr4k = matches['4k'];
      if (matches.hd) movie.radarrHd = matches.hd;
      const row = picker.closest('tr');
      row?.querySelector('.show-status')?.replaceWith(createStatusIcon(movie));
      const columns = rowColumns();
      for (const quality of columns) {
        const oldCell = row?.querySelector(`.radarr-cell-${quality}`);
        if (oldCell) oldCell.replaceWith(createRadarrCell(movie, quality, columns));
      }
      if (row) {
        const fourK = get4KSize(movie);
        const hd = getHDSize(movie);
        if (fourK > 0) row.dataset.fourKSize = String(fourK);
        if (hd > 0) row.dataset.hdSize = String(hd);
      }
    } catch (error) {
      button.disabled = false;
      status.textContent = error.message;
    }
  }

  const search = () => load(input.value.trim());
  searchButton.addEventListener('click', search);
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') search();
  });
  load('');
}

// --- Size helpers ---

/**
 * Returns the file size in bytes for a 4K video from a movie object or DOM element.
 * @param {Object|Element} item - Movie object or DOM row element.
 * @returns {number} Size in bytes (or 0 if unavailable).
 */
function get4KSize(item) {
  if (!item) return 0;
  if (typeof item === 'object') {
    if (typeof Element !== 'undefined' && item instanceof Element) {
      if (item.dataset && item.dataset.fourKSize) {
        return Number(item.dataset.fourKSize) || 0;
      }
      const sizeSpan = item.querySelector('.radarr-cell-4k .radarr-match-size') || item.querySelector('.radarr-match-size');
      if (sizeSpan && sizeSpan.dataset && sizeSpan.dataset.sizeBytes) {
        return Number(sizeSpan.dataset.sizeBytes) || 0;
      }
    }
    if (item.radarr4k && Number.isFinite(item.radarr4k.sizeBytes) && item.radarr4k.sizeBytes > 0) {
      return item.radarr4k.sizeBytes;
    }
    if (Number.isFinite(item.fourKSizeBytes) && item.fourKSizeBytes > 0) {
      return item.fourKSizeBytes;
    }
    if (Number.isFinite(item.sizeBytes) && item.sizeBytes > 0) {
      return item.sizeBytes;
    }
    if (Number.isFinite(item.size) && item.size > 0) {
      return item.size;
    }
  }
  return 0;
}

/**
 * Returns the HD file size in bytes for a movie object or DOM element.
 * @param {Object|Element} item - Movie object or DOM row element.
 * @returns {number} Size in bytes (or 0 if unavailable).
 */
function getHDSize(item) {
  if (!item) return 0;
  if (typeof item === 'object') {
    if (typeof Element !== 'undefined' && item instanceof Element) {
      if (item.dataset && item.dataset.hdSize) {
        return Number(item.dataset.hdSize) || 0;
      }
      const sizeSpan = item.querySelector('.radarr-cell-hd .radarr-match-size');
      if (sizeSpan && sizeSpan.dataset && sizeSpan.dataset.sizeBytes) {
        return Number(sizeSpan.dataset.sizeBytes) || 0;
      }
    }
    if (item.radarrHd && Number.isFinite(item.radarrHd.sizeBytes) && item.radarrHd.sizeBytes > 0) {
      return item.radarrHd.sizeBytes;
    }
  }
  return 0;
}

/**
 * Looks at the file sizes for all the 4K videos and highlights the top 20% (or specified percentile).
 * Operates on passed movie items, analysis data, DOM table rows, or the current view.
 *
 * @param {Array|Object|NodeList|number} [source] - Optional data source or percentile.
 * @param {number} [percentile=0.20] - Ratio of top videos to highlight (default: 0.20 for top 20%).
 * @returns {Array} Array of top highlighted movies or items, with metadata (.threshold, .count, .total4K).
 */
function highlightTop4KVideos(source, percentile = 0.20) {
  if (typeof source === 'number') {
    percentile = source;
    source = null;
  }
  if (!Number.isFinite(percentile) || percentile <= 0) {
    percentile = 0.20;
  }

  let items = [];
  let isDOMMode = false;

  if (Array.isArray(source)) {
    items = source;
  } else if (source && Array.isArray(source.matches)) {
    items = source.matches;
  } else if (typeof NodeList !== 'undefined' && source instanceof NodeList) {
    items = Array.from(source);
    isDOMMode = true;
  } else if (currentAnalysis && Array.isArray(currentAnalysis.matches) && currentAnalysis.matches.length > 0) {
    items = currentAnalysis.matches;
  } else if (typeof document !== 'undefined') {
    const rows = document.querySelectorAll('#plex-results-body tr');
    if (rows.length > 0) {
      items = Array.from(rows);
      isDOMMode = true;
    }
  }

  const valid = [];
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    const size = get4KSize(item);
    if (size > 0) {
      valid.push({ item, size, index: i });
    }
  }

  if (valid.length === 0) {
    if (typeof document !== 'undefined') {
      const rows = document.querySelectorAll('#plex-results-body tr');
      for (const row of rows) {
        row.classList.remove('is-top-4k', 'top-4k-highlight');
        const cell4k = row.querySelector('.radarr-cell-4k') || row.cells?.[3];
        if (cell4k) cell4k.classList.remove('is-top-4k');
        const badge = row.querySelector('.top-size-badge');
        if (badge) badge.remove();
        const sizeSpan = row.querySelector('.radarr-match-size');
        if (sizeSpan) sizeSpan.classList.remove('is-top-4k', 'top-4k-size');
      }
      if (highlightInfo) highlightInfo.hidden = true;
    }
    const emptyResult = [];
    emptyResult.threshold = 0;
    emptyResult.count = 0;
    emptyResult.total4K = 0;
    return emptyResult;
  }

  valid.sort((a, b) => b.size - a.size);

  const topCount = Math.max(1, Math.ceil(valid.length * percentile));
  const threshold = valid[topCount - 1].size;

  const topItemsSet = new Set();
  const highlighted = [];

  for (const entry of valid) {
    if (entry.size >= threshold && entry.size > 0) {
      topItemsSet.add(entry.item);
      highlighted.push(entry.item);
      if (typeof entry.item === 'object' && entry.item !== null) {
        entry.item.isTop20Percent4k = true;
      }
    } else if (typeof entry.item === 'object' && entry.item !== null) {
      entry.item.isTop20Percent4k = false;
    }
  }

  if (typeof document !== 'undefined') {
    const rows = document.querySelectorAll('#plex-results-body tr');
    rows.forEach((row, idx) => {
      let isTop = false;
      if (isDOMMode) {
        isTop = topItemsSet.has(row);
      } else if (idx < items.length) {
        isTop = topItemsSet.has(items[idx]);
      } else {
        const rowSize = get4KSize(row);
        isTop = rowSize >= threshold && rowSize > 0;
      }

      row.classList.toggle('is-top-4k', isTop);
      row.classList.toggle('top-4k-highlight', isTop);

      const cell4k = row.querySelector('.radarr-cell-4k') || row.cells?.[3];
      if (cell4k) {
        cell4k.classList.toggle('is-top-4k', isTop);
      }

      const sizeSpan = row.querySelector('.radarr-cell-4k .radarr-match-size') || row.querySelector('.radarr-match-size');
      if (sizeSpan) {
        sizeSpan.classList.toggle('is-top-4k', isTop);
        sizeSpan.classList.toggle('top-4k-size', isTop);
      }

      const existingBadge = row.querySelector('.top-size-badge');
      if (isTop) {
        if (!existingBadge && sizeSpan) {
          const badge = document.createElement('span');
          badge.className = 'top-size-badge';
          badge.textContent = 'Top 20%';
          badge.title = 'Top 20% largest 4K video';
          sizeSpan.append(badge);
        }
      } else if (existingBadge) {
        existingBadge.remove();
      }
    });

    if (highlightInfo) {
      if (highlighted.length > 0) {
        const movieWord = highlighted.length === 1 ? 'movie' : 'movies';
        highlightInfo.textContent = `Top 20% 4K file sizes highlighted (${highlighted.length} ${movieWord} ≥ ${formatFileSize(threshold)})`;
        highlightInfo.hidden = false;
      } else {
        highlightInfo.hidden = true;
      }
    }
  }

  highlighted.threshold = threshold;
  highlighted.count = highlighted.length;
  highlighted.total4K = valid.length;
  return highlighted;
}

function formatFileSize(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return 'Size unavailable';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const digits = unitIndex === 0 ? 0 : 1;
  return `${value.toFixed(digits)} ${units[unitIndex]}`;
}

function is4K(resolution) {
  return resolution.toLowerCase() === '4k' || resolution.startsWith('2160');
}

if (typeof window !== 'undefined') {
  window.highlightTop4KVideos = highlightTop4KVideos;
  window.highlightTop4K = highlightTop4KVideos;
  window.highlightTop20Percent4K = highlightTop4KVideos;
  window.highlightTop4KSizes = highlightTop4KVideos;
  window.get4KSize = get4KSize;
  window.getHDSize = getHDSize;
  window.compositeRating = compositeRating;
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    highlightTop4KVideos,
    formatFileSize,
    is4K,
    get4KSize,
    getHDSize,
    compositeRating,
  };
}

if (doc) {
  refreshStatus().catch((error) => showMessage(error.message, 'error'));
  loadStorage();
}
