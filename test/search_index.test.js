const assert = require('assert');
const { letterFor } = require('../internal/web/assets/app.js');

console.log('Running A-Z index tests...');

assert.strictEqual(letterFor('Alien'), 'A');
assert.strictEqual(letterFor('a bug\'s life'), 'A');
assert.strictEqual(letterFor('Zodiac'), 'Z');
assert.strictEqual(letterFor('Émile'), 'E', 'accents file under the base letter');
assert.strictEqual(letterFor('300'), '#', 'numbers file under #');
assert.strictEqual(letterFor('(500) Days of Summer'), '#', 'leading symbols are skipped, then the digit decides');
assert.strictEqual(letterFor('"Weird" Title'), 'W', 'leading punctuation is skipped');
assert.strictEqual(letterFor('!!!'), '#');
assert.strictEqual(letterFor(''), '#');
assert.strictEqual(letterFor(undefined), '#');
console.log('✓ letterFor passed');
