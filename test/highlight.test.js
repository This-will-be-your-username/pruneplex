const assert = require('assert');
const { highlightTop4KVideos, get4KSize, formatFileSize } = require('../internal/web/assets/app.js');

console.log('Running highlightTop4KVideos tests...');

// Test 1: Empty input
{
  const res = highlightTop4KVideos([]);
  assert.strictEqual(res.length, 0);
  assert.strictEqual(res.threshold, 0);
  assert.strictEqual(res.count, 0);
  assert.strictEqual(res.total4K, 0);
  console.log('✓ Test 1: Empty input passed');
}

// Test 2: No 4K sizes available
{
  const movies = [
    { title: 'A', resolutions: ['4k'] },
    { title: 'B', resolutions: ['1080p'] },
  ];
  const res = highlightTop4KVideos(movies);
  assert.strictEqual(res.length, 0);
  assert.strictEqual(movies[0].isTop20Percent4k, undefined);
  console.log('✓ Test 2: No 4K sizes passed');
}

// Test 3: Top 20% of 5 movies is 1 movie
{
  const movies = [
    { title: 'Movie 10GB', radarr4k: { sizeBytes: 10 * 1024 * 1024 * 1024 } },
    { title: 'Movie 20GB', radarr4k: { sizeBytes: 20 * 1024 * 1024 * 1024 } },
    { title: 'Movie 30GB', radarr4k: { sizeBytes: 30 * 1024 * 1024 * 1024 } },
    { title: 'Movie 40GB', radarr4k: { sizeBytes: 40 * 1024 * 1024 * 1024 } },
    { title: 'Movie 50GB', radarr4k: { sizeBytes: 50 * 1024 * 1024 * 1024 } },
  ];
  const res = highlightTop4KVideos(movies);
  assert.strictEqual(res.length, 1);
  assert.strictEqual(res[0].title, 'Movie 50GB');
  assert.strictEqual(movies[4].isTop20Percent4k, true);
  assert.strictEqual(movies[3].isTop20Percent4k, false);
  assert.strictEqual(res.threshold, 50 * 1024 * 1024 * 1024);
  assert.strictEqual(res.total4K, 5);
  console.log('✓ Test 3: 5 movies -> 1 highlighted passed');
}

// Test 4: Top 20% of 10 movies is 2 movies
{
  const movies = [];
  for (let i = 1; i <= 10; i++) {
    movies.push({
      title: `Movie ${i}`,
      radarr4k: { sizeBytes: i * 1000 },
    });
  }
  const res = highlightTop4KVideos(movies);
  assert.strictEqual(res.length, 2);
  assert.strictEqual(res[0].title, 'Movie 10');
  assert.strictEqual(res[1].title, 'Movie 9');
  assert.strictEqual(res.threshold, 9000);
  console.log('✓ Test 4: 10 movies -> 2 highlighted passed');
}

// Test 5: Ties at the cutoff threshold
{
  const movies = [
    { title: 'A', radarr4k: { sizeBytes: 80 } },
    { title: 'B', radarr4k: { sizeBytes: 80 } },
    { title: 'C', radarr4k: { sizeBytes: 40 } },
    { title: 'D', radarr4k: { sizeBytes: 20 } },
    { title: 'E', radarr4k: { sizeBytes: 10 } },
  ];
  const res = highlightTop4KVideos(movies);
  assert.strictEqual(res.length, 2, 'both tied movies should be highlighted');
  assert.strictEqual(movies[0].isTop20Percent4k, true);
  assert.strictEqual(movies[1].isTop20Percent4k, true);
  assert.strictEqual(movies[2].isTop20Percent4k, false);
  console.log('✓ Test 5: Boundary ties passed');
}

// Test 6: Fallback to fourKSizeBytes / sizeBytes
{
  const movies = [
    { title: 'A', fourKSizeBytes: 500 },
    { title: 'B', sizeBytes: 200 },
    { title: 'C', fourKSizeBytes: 100 },
  ];
  const res = highlightTop4KVideos(movies);
  assert.strictEqual(res.length, 1);
  assert.strictEqual(res[0].title, 'A');
  console.log('✓ Test 6: Fallback sizes passed');
}

// Test 7: Analysis wrapper object { matches: [...] }
{
  const analysis = {
    scanned: 100,
    matches: [
      { title: 'M1', radarr4k: { sizeBytes: 10 } },
      { title: 'M2', radarr4k: { sizeBytes: 50 } },
    ],
  };
  const res = highlightTop4KVideos(analysis);
  assert.strictEqual(res.length, 1);
  assert.strictEqual(res[0].title, 'M2');
  console.log('✓ Test 7: Analysis wrapper object passed');
}

console.log('\nAll highlightTop4KVideos unit tests passed successfully!');
