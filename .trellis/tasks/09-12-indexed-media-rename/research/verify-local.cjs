// Fast source/script checks only. Go, ABI, runtime and playback checks require CI.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const {execFileSync} = require('node:child_process');

const repo = path.resolve(__dirname, '../../../..');
const base = 'ab2217bccb46b8ca854b778ef82cd805e6941cb9';
const read = file => fs.readFileSync(path.join(repo, file), 'utf8').replace(/\r\n/g, '\n');
const original = file => execFileSync('git', ['show', base + ':' + file], {
  cwd: repo, encoding: 'utf8', maxBuffer: 2 * 1024 * 1024,
}).replace(/\r\n/g, '\n');
const renamed = value => value.replaceAll('indexedVod', 'indexedMedia')
  .replaceAll('indexed-vod', 'indexed-media')
  .replaceAll('Indexed VOD', 'Indexed Media')
  .replaceAll('indexed VOD', 'indexed media');

for (const file of [
  'go.sum', 'VERSION', 'compatibility.json', 'plugin-abi-release.json',
  'source.go', 'sidx.go', 'producer_test.go', 'sidx_test.go',
  'tests/e2e/fixture-server.go', 'tests/e2e/fixture-server_test.go',
  'tests/e2e/config.conf', '.github/workflows/release.yml',
]) {
  assert.equal(read(file), original(file), file + ': invariant changed');
}
for (const file of ['go.mod', 'node.go', 'plugin.go', 'producer.go']) {
  assert.equal(read(file), renamed(original(file)), file + ': non-naming source change');
}
console.log('PASS: production source is naming-only; identity/dependency/version/release invariants unchanged');

for (const file of [
  'examples/youtube-hls/chain.json', 'testdata/smoke-node-pool.json',
  'testdata/smoke-borrower-chain.json', 'tests/e2e/node-pool.json',
]) {
  assert.deepEqual(JSON.parse(read(file)), JSON.parse(renamed(original(file))),
    file + ': changes exceed the approved naming map');
}
const graph = JSON.parse(read('examples/youtube-hls/chain.json'));
const nodes = new Map(graph.metadata.nodes.map(node => [node.id, node]));
let scriptCount = 0;
for (const node of nodes.values()) {
  if (node.configuration?.jsScript) {
    new vm.Script('(function(msg,metadata,msgType,dataType){' + node.configuration.jsScript + '\n})');
    scriptCount++;
  }
}
console.log('PASS: shipped graph/pool JSON follows the exact naming map; ' + scriptCount + ' scripts parse');

const now = 1800000000000;
class FixtureDate extends Date { static now() { return now; } }
const cache = new Map();
const ttls = new Map();
const api = {
  Has: key => cache.has(key),
  Get: key => cache.get(key),
  Set: (key, value, ttl) => { cache.set(key, value); ttls.set(key, ttl); },
  Delete: key => { cache.delete(key); ttls.delete(key); },
};
function run(id, msg, metadata) {
  const node = nodes.get(id);
  assert.ok(node?.configuration?.jsScript, 'missing real graph script: ' + id);
  const value = vm.runInNewContext(
    '(function(){' + node.configuration.jsScript + '\n})()',
    {msg, metadata: {...metadata}, msgType: 'test', dataType: 'JSON', Date: FixtureDate,
      $ctx: {ChainCache: () => api}},
    {timeout: 1000},
  );
  return JSON.parse(JSON.stringify(value));
}

const videoId = 'Z4tHPyZBC8g';
const revision = 'a'.repeat(64);
const lease = {
  sourceKey: 'youtube:' + videoId + ':avc-1080-m4a',
  video: {url: 'https://media.invalid/video', container: 'mp4', codec: 'h264'},
  audio: {url: 'https://media.invalid/audio', container: 'm4a', codec: 'aac'},
};
const manifestKey = 'indexed-media:manifest:youtube:' + videoId + ':avc-1080-m4a:indexed-ts-v2';
const leaseKey = 'indexed-media:lease:' + videoId + ':' + revision;
const legacyManifestKey = manifestKey.replace('indexed-media:', 'indexed-vod:');
const legacyLeaseKey = leaseKey.replace('indexed-media:', 'indexed-vod:');
const metadata = {
  videoId, revision, source: JSON.stringify(lease), leaseExpiresAtMs: String(now + 60000),
  segment: '1', stagingDir: '/data/resource-origin/staging/fixture', maxBytes: '1024',
  publishBy: '2027-01-15T08:05:00Z', manifestCacheKey: manifestKey,
};

assert.equal(run('manifest-request', {}, {videoId}).msg.cached, false);
assert.deepEqual(run('segment-source-switch', {}, metadata), ['Resolve']);
cache.set(legacyLeaseKey, JSON.stringify(lease));
cache.set(legacyManifestKey, JSON.stringify({
  revision, manifest: '#EXTM3U\nlegacy\n', leaseExpiresAtMs: now + 60000,
}));
assert.equal(run('manifest-request', {}, {videoId}).msg.cached, false);
assert.deepEqual(run('segment-source-switch', {}, metadata), ['Resolve']);
console.log('PASS: cold routes resolve; populated old manifest/lease namespaces are ignored');

const acquired = run('parent-acquire-request', {
  revision, segments: [{duration: 2}, {duration: 3}],
}, metadata);
assert.equal(cache.get(leaseKey), JSON.stringify(lease));
assert.equal(ttls.get(leaseKey), '60000ms');
assert.equal(acquired.msg.operation, 'acquire');
assert.equal(acquired.msg.fingerprint, 'indexed-ts-v2:' + revision);
const published = run('manifest-response', {}, acquired.metadata);
assert.ok(published.msg.includes('#EXT-X-PLAYLIST-TYPE:VOD'));
assert.equal(ttls.get(manifestKey), '60000ms');
const warm = run('manifest-request', {}, {videoId});
assert.equal(warm.msg.cached, true);
assert.equal(warm.metadata.revision, revision);
assert.equal(warm.metadata.manifest, published.msg);
assert.deepEqual(run('segment-source-switch', {}, metadata), ['Cached']);
const produced = run('cached-produce-request', {}, metadata);
assert.deepEqual(produced.msg, {
  operation: 'produce', source: lease, expectedRevision: revision, segment: 1,
  stagingDir: metadata.stagingDir, maxBytes: 1024, publishBy: metadata.publishBy,
});
console.log('PASS: real writers/readers share new namespaces, TTLs and unchanged paired production payload');

const refreshed = run('refresh-request', {}, metadata);
assert.equal(refreshed.metadata.phase, 'refresh');
assert.equal(cache.has(leaseKey), false);
assert.equal(cache.has(manifestKey), false);
assert.equal(cache.has(legacyLeaseKey), true);
assert.equal(cache.has(legacyManifestKey), true);
assert.equal(run('manifest-request', {}, {videoId}).msg.cached, false);
assert.deepEqual(run('segment-source-switch', {}, metadata), ['Resolve']);
console.log('PASS: stale refresh evicts both new keys and returns to normal resolution without legacy fallback');
console.log('LIMIT: script/source checks do not execute Go tests or prove RuleGo/ABI/HLS playback');
