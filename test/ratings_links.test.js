const assert = require('assert');
const { ratingUrl } = require('../internal/web/assets/app.js');

console.log('Running rating link tests...');

const movie = { title: 'Blade Runner 2049', imdbId: 'tt1856101', tmdbId: 335984 };

assert.strictEqual(ratingUrl(movie, { source: 'imdb' }), 'https://www.imdb.com/title/tt1856101/');
assert.strictEqual(ratingUrl(movie, { source: 'tmdb' }), 'https://www.themoviedb.org/movie/335984');
assert.strictEqual(ratingUrl(movie, { source: 'themoviedb' }), 'https://www.themoviedb.org/movie/335984');
assert.strictEqual(
  ratingUrl(movie, { source: 'rotten_tomatoes' }),
  'https://www.rottentomatoes.com/search?search=Blade%20Runner%202049',
  'Rotten Tomatoes has no ID from Plex, so it links to a title search',
);
assert.strictEqual(ratingUrl(movie, { source: 'rotten_tomatoes_audience' }), ratingUrl(movie, { source: 'rotten_tomatoes' }));
assert.strictEqual(ratingUrl(movie, { source: 'metacritic' }), 'https://www.metacritic.com/search/Blade%20Runner%202049/?category=2');
assert.strictEqual(ratingUrl(movie, { source: 'trakt' }), 'https://trakt.tv/search/imdb/tt1856101');
assert.strictEqual(
  ratingUrl(movie, { source: 'trakt', url: 'https://trakt.tv/movies/blade-runner-2049-2017' }),
  'https://trakt.tv/movies/blade-runner-2049-2017',
  'a provider-supplied https URL wins',
);
assert.strictEqual(
  ratingUrl(movie, { source: 'trakt', url: 'javascript:alert(1)' }),
  'https://trakt.tv/search/imdb/tt1856101',
  'only https URLs are trusted',
);
assert.strictEqual(ratingUrl({ title: 'No Ids' }, { source: 'imdb' }), 'https://www.imdb.com/find/?q=No%20Ids&s=tt', 'falls back to a search without an ID');
assert.strictEqual(ratingUrl(movie, { source: 'user' }), '', 'Plex user ratings have no page');
assert.strictEqual(ratingUrl(movie, null), '');
console.log('✓ ratingUrl passed');
