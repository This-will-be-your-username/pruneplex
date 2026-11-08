const assert = require('assert');

// Simple DOM mock
class MockElement {
  constructor(tagName) {
    this.tagName = tagName.toUpperCase();
    this.children = [];
    this.parentElement = null;
    this.classList = new Set();
    this.dataset = {};
    this.textContent = '';
    this.hidden = false;
    this._classes = new Set();
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

  set classList(_) {
    this._classes = new Set();
  }

  querySelector(selector) {
    if (selector === '#plex-results-body') return resultsBody;
    if (selector === '#plex-highlight-info') return highlightInfo;
    for (const child of this.children) {
      if (child.matches(selector)) return child;
      const found = child.querySelector(selector);
      if (found) return found;
    }
    return null;
  }

  querySelectorAll(selector) {
    if (selector === '#plex-results-body tr') return rows;
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

  remove() {
    if (this.parentElement) {
      const idx = this.parentElement.children.indexOf(this);
      if (idx !== -1) this.parentElement.children.splice(idx, 1);
      this.parentElement = null;
    }
  }
}

// Setup global mock
global.Element = MockElement;
global.document = {
  createElement: (tag) => new MockElement(tag),
  querySelector: (sel) => root.querySelector(sel),
  querySelectorAll: (sel) => root.querySelectorAll(sel),
};

const root = new MockElement('div');
const resultsBody = new MockElement('tbody');
resultsBody.id = 'plex-results-body';
root.append(resultsBody);

const highlightInfo = new MockElement('span');
highlightInfo.id = 'plex-highlight-info';
root.append(highlightInfo);

// Create 5 rows
const rows = [];
const sizes = [10, 20, 30, 40, 50];
for (let i = 0; i < 5; i++) {
  const tr = new MockElement('tr');
  tr.dataset.fourKSize = String(sizes[i] * 1024 * 1024 * 1024);
  const tdTitle = new MockElement('th');
  tdTitle.textContent = `Movie ${sizes[i]}`;
  const tdLib = new MockElement('td');
  const tdRes = new MockElement('td');
  const td4K = new MockElement('td');
  td4K.className = 'radarr-cell-4k';
  td4K.classList.add('radarr-cell-4k');
  const matchDiv = new MockElement('div');
  matchDiv.classList.add('radarr-match');
  const sizeSpan = new MockElement('span');
  sizeSpan.classList.add('radarr-match-size');
  sizeSpan.dataset.sizeBytes = String(sizes[i] * 1024 * 1024 * 1024);
  matchDiv.append(sizeSpan);
  td4K.append(matchDiv);
  const tdHD = new MockElement('td');
  tr.append(tdTitle, tdLib, tdRes, td4K, tdHD);
  resultsBody.append(tr);
  rows.push(tr);
}

// Load app
delete require.cache[require.resolve('../internal/web/assets/app.js')];
const { highlightTop4KVideos } = require('../internal/web/assets/app.js');

const res = highlightTop4KVideos();
assert.strictEqual(res.length, 1);
assert.strictEqual(rows[4].classList.contains('is-top-4k'), true, 'row 5 (50GB) should be highlighted');
assert.strictEqual(rows[3].classList.contains('is-top-4k'), false, 'row 4 (40GB) should not be highlighted');
assert.strictEqual(rows[4].querySelector('.top-size-badge') !== null, true, 'row 5 should have top-size-badge');
assert.strictEqual(rows[3].querySelector('.top-size-badge'), null, 'row 4 should not have top-size-badge');
assert.strictEqual(highlightInfo.hidden, false, 'highlightInfo should be visible');
assert.ok(highlightInfo.textContent.includes('Top 20% 4K file sizes highlighted'));

console.log('✓ DOM highlighting test passed successfully!');
