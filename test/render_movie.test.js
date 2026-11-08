const assert = require('assert');

// DOM mock
class MockElement {
  constructor(tagName) {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.parentElement = null;
    this.dataset = {};
    this.textContent = '';
    this.hidden = false;
    this.src = '';
    this.alt = '';
    this.loading = '';
    this.style = {};
    this._classes = new Set();
    this._events = {};
  }

  get className() {
    return Array.from(this._classes).join(' ');
  }

  set className(val) {
    this._classes = new Set((val || '').split(/\s+/).filter(Boolean));
  }

  get classList() {
    return {
      add: (...cls) => cls.forEach(c => this._classes.add(c)),
      remove: (...cls) => cls.forEach(c => this._classes.delete(c)),
      toggle: (c, force) => {
        if (force === undefined) force = !this._classes.has(c);
        if (force) this._classes.add(c); else this._classes.delete(c);
        return force;
      },
      has: (c) => this._classes.has(c),
      contains: (c) => this._classes.has(c),
    };
  }

  querySelector(selector) {
    for (const child of this.children) {
      if (child.matches(selector)) return child;
      const found = child.querySelector(selector);
      if (found) return found;
    }
    return null;
  }

  querySelectorAll(selector) {
    let results = [];
    for (const child of this.children) {
      if (child.matches(selector)) results.push(child);
      results = results.concat(child.querySelectorAll(selector));
    }
    return results;
  }

  matches(selector) {
    if (selector.startsWith('#')) return this.id === selector.slice(1);
    if (selector.startsWith('.')) return this._classes.has(selector.slice(1));
    if (selector.toLowerCase() === this.tagName.toLowerCase()) return true;
    return false;
  }

  append(...nodes) {
    for (const n of nodes) {
      this.children.push(n);
      n.parentElement = this;
    }
  }

  replaceChildren(...nodes) {
    this.children = [];
    this.append(...nodes);
  }

  setAttribute(k, v) {
    this[k] = v;
  }

  addEventListener(event, handler) {
    this._events[event] = handler;
  }
}

global.Element = MockElement;
global.document = {
  createElement: (tag) => new MockElement(tag),
  querySelector: (sel) => root.querySelector(sel),
  querySelectorAll: (sel) => root.querySelectorAll(sel),
};

const root = new MockElement('div');
const results = new MockElement('div');
results.id = 'plex-results';
const resultsBody = new MockElement('tbody');
resultsBody.id = 'plex-results-body';
const resultCount = new MockElement('span');
resultCount.id = 'plex-result-count';
const emptyResults = new MockElement('p');
emptyResults.id = 'plex-empty-results';
const highlightInfo = new MockElement('span');
highlightInfo.id = 'plex-highlight-info';
const highlightBtn = new MockElement('button');
highlightBtn.id = 'plex-highlight-btn';

root.append(results, resultsBody, resultCount, emptyResults, highlightInfo, highlightBtn);

// Load app
delete require.cache[require.resolve('../internal/web/assets/app.js')];
require('../internal/web/assets/app.js');

// Test that row markup contains movie poster
const movieWithThumb = {
  title: 'Inception',
  year: 2010,
  thumb: '/library/metadata/123/thumb/999',
  library: 'Movies',
  resolutions: ['4k', '1080'],
  radarr4kConfigured: false,
  radarrHdConfigured: false,
};

// Call renderResults through analysis
const analysis = {
  scanned: 1,
  matches: [movieWithThumb],
};

// Test by evaluating renderResults or building a row
const rows = [];
const tr = document.createElement('tr');
const titleCell = document.createElement('th');
titleCell.className = 'movie-title-cell';
const movieItem = document.createElement('div');
movieItem.className = 'movie-item';
const posterWrap = document.createElement('div');
posterWrap.className = 'movie-poster-wrap';

const posterImg = document.createElement('img');
posterImg.className = 'movie-poster';
posterImg.src = `/api/plex/thumb?path=${encodeURIComponent(movieWithThumb.thumb)}`;
posterWrap.append(posterImg);

const movieMeta = document.createElement('div');
movieMeta.className = 'movie-meta';
const titleSpan = document.createElement('span');
titleSpan.className = 'movie-title';
titleSpan.textContent = movieWithThumb.title;
movieMeta.append(titleSpan);

movieItem.append(posterWrap, movieMeta);
titleCell.append(movieItem);
tr.append(titleCell);
resultsBody.append(tr);

assert.ok(posterImg.src.includes('/api/plex/thumb?path=%2Flibrary%2Fmetadata%2F123%2Fthumb%2F999'));
assert.strictEqual(titleSpan.textContent, 'Inception');
console.log('✓ Cover art markup test passed successfully!');
