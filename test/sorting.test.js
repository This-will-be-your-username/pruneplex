const assert = require('assert');
const {
  criticScore, audienceScore, compositeOfTwo, sortMovies, formatAdded, addedInfo, looksLikeReleaseDate,
} = require('../internal/web/assets/app.js');

console.log('Running sorting tests...');

const close = (actual, expected) => assert.ok(Math.abs(actual - expected) < 1e-9, `${actual} should be ${expected}`);

// --- critic / audience / composite scores ---
const rated = {
  ratings: [
    { source: 'rotten_tomatoes', type: 'critic', value: 9.3 },
    { source: 'rotten_tomatoes_audience', type: 'audience', value: 8.4 },
    { source: 'imdb', type: 'audience', value: 7.2 },
    { source: 'user', type: 'user', value: 9 },
  ],
};
close(criticScore(rated), 93);
close(audienceScore(rated), 78); // mean of 84 and 72; the personal user rating is ignored
close(compositeOfTwo(rated), 85.5); // mean of the critic and audience scores
assert.strictEqual(criticScore({ ratings: [{ type: 'audience', value: 8 }] }), null, 'no critic ratings');
close(compositeOfTwo({ ratings: [{ type: 'critic', value: 6 }] }), 60); // only one kind: just that one
assert.strictEqual(compositeOfTwo({}), null);
assert.strictEqual(compositeOfTwo({ ratings: [{ type: 'user', value: 9 }] }), null);
close(criticScore({ ratings: [{ type: 'critic', value: 88 }] }), 88); // already 0-100
console.log('✓ scores passed');

// --- ordering ---
const movies = [
  { title: 'Bravo', year: 2000, addedAt: 300, radarr4k: { sizeBytes: 50 }, radarrHd: { sizeBytes: 5 }, ratings: [{ type: 'critic', value: 5 }, { type: 'audience', value: 9 }] },
  { title: 'alpha', year: 2001, addedAt: 100, radarr4k: { sizeBytes: 10 }, radarrHd: null, ratings: [{ type: 'critic', value: 9 }] },
  { title: 'Charlie', year: 2002, addedAt: 0, radarr4k: null, radarrHd: { sizeBytes: 20 }, ratings: [] },
  { title: 'Delta', year: 2003, addedAt: 200, radarr4k: { sizeBytes: 30 }, radarrHd: { sizeBytes: 15 }, ratings: [{ type: 'audience', value: 8 }] },
];
const titles = (state) => sortMovies(movies, state).map((m) => m.title).join(',');

assert.strictEqual(titles({ key: 'title', dir: 'asc' }), 'alpha,Bravo,Charlie,Delta', 'A-Z ignores case');
assert.strictEqual(titles({ key: 'title', dir: 'desc' }), 'Delta,Charlie,Bravo,alpha', 'Z-A');

// Date added: movies with no date are always last, whichever way you sort.
assert.strictEqual(titles({ key: 'added', dir: 'desc' }), 'Bravo,Delta,alpha,Charlie', 'newest first');
assert.strictEqual(titles({ key: 'added', dir: 'asc' }), 'alpha,Delta,Bravo,Charlie', 'oldest first');

// Radarr file sizes: no match (or no size) is last in both directions.
assert.strictEqual(titles({ key: 'size4k', dir: 'desc' }), 'Bravo,Delta,alpha,Charlie');
assert.strictEqual(titles({ key: 'size4k', dir: 'asc' }), 'alpha,Delta,Bravo,Charlie');
assert.strictEqual(titles({ key: 'sizeHd', dir: 'desc' }), 'Charlie,Delta,Bravo,alpha');
assert.strictEqual(titles({ key: 'sizeHd', dir: 'asc' }), 'Bravo,Delta,Charlie,alpha');

// Ratings: critic, audience, composite, each high to low and low to high.
assert.strictEqual(titles({ key: 'ratings', mode: 'critic', dir: 'desc' }), 'alpha,Bravo,Charlie,Delta', 'critic high to low; unrated last');
assert.strictEqual(titles({ key: 'ratings', mode: 'critic', dir: 'asc' }), 'Bravo,alpha,Charlie,Delta', 'critic low to high; unrated last');
assert.strictEqual(titles({ key: 'ratings', mode: 'audience', dir: 'desc' }), 'Bravo,Delta,alpha,Charlie');
assert.strictEqual(titles({ key: 'ratings', mode: 'audience', dir: 'asc' }), 'Delta,Bravo,alpha,Charlie');
// composite: Bravo (50+90)/2 = 70, alpha 90 (critic only), Delta 80 (audience only), Charlie none
assert.strictEqual(titles({ key: 'ratings', mode: 'composite', dir: 'desc' }), 'alpha,Delta,Bravo,Charlie');
assert.strictEqual(titles({ key: 'ratings', mode: 'composite', dir: 'asc' }), 'Bravo,Delta,alpha,Charlie');

// Equal values fall back to title order, and sorting never changes the input.
const ties = [{ title: 'B', year: 1, addedAt: 5 }, { title: 'A', year: 1, addedAt: 5 }];
assert.strictEqual(sortMovies(ties, { key: 'added', dir: 'desc' }).map((m) => m.title).join(''), 'AB');
assert.strictEqual(ties[0].title, 'B');
console.log('✓ ordering passed');

// --- date formatting ---
const text = formatAdded(1700000000); // 14-15 Nov 2023 depending on the time zone
assert.ok(/2023/.test(text) && /1[45]/.test(text), `unexpected date text: ${text}`);
console.log('✓ formatAdded passed');

// --- arrival date: Radarr/Sonarr import date beats Plex's file-timestamp date ---
const plexOnly = { addedAt: 1000 };
assert.deepStrictEqual(addedInfo(plexOnly), { seconds: 1000, source: 'Plex' });
assert.deepStrictEqual(addedInfo({ addedAt: 1000, radarr4k: { addedAt: 5000 } }), { seconds: 5000, source: 'Radarr' }, 'the import date wins over Plex');
assert.deepStrictEqual(
  addedInfo({ addedAt: 1000, radarr4k: { addedAt: 9000 }, radarrHd: { addedAt: 4000 } }),
  { seconds: 4000, source: 'Radarr' },
  'with a 4K and an HD copy, the earlier import is when it first arrived',
);
assert.deepStrictEqual(addedInfo({ addedAt: 1000, radarr4k: { sizeBytes: 5 } }), { seconds: 1000, source: 'Plex' }, 'a match with no date falls back to Plex');
assert.strictEqual(addedInfo({}), null);
// sorting uses the same date
const arrived = [
  { title: 'Release date in Plex', addedAt: 100, radarr4k: { addedAt: 900 } },
  { title: 'Plex only', addedAt: 500 },
];
assert.strictEqual(sortMovies(arrived, { key: 'added', dir: 'desc' })[0].title, 'Release date in Plex');
console.log('✓ addedInfo passed');

// A Plex date exactly on midnight UTC is a release date Radarr wrote onto the file, not an arrival date.
const midnight = 86400 * 20000;
assert.ok(looksLikeReleaseDate(midnight));
assert.ok(!looksLikeReleaseDate(midnight + 1));
assert.ok(!looksLikeReleaseDate(0));
assert.strictEqual(addedInfo({ addedAt: midnight }), null, 'not trusted, so unknown');
assert.deepStrictEqual(addedInfo({ addedAt: midnight, radarr4k: { addedAt: 5000 } }), { seconds: 5000, source: 'Radarr' });
const unsure = [{ title: 'Release-date stamp', addedAt: midnight }, { title: 'Real', addedAt: 1700000001 }];
assert.strictEqual(sortMovies(unsure, { key: 'added', dir: 'asc' })[0].title, 'Real', 'untrusted dates sort last either way');
console.log('✓ release-date detection passed');
