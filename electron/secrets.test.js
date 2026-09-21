const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { test } = require('node:test');
const { loadOrCreateSecrets } = require('./secrets');

function temporaryDirectory(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'heqiuyu-secrets-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  return directory;
}

test('new secrets remain stable across restarts without rewriting the file', (t) => {
  const directory = temporaryDirectory(t);
  const secrets = loadOrCreateSecrets(directory);
  assert.ok(secrets.SESSION_SECRET.length >= 32);
  assert.ok(secrets.CRYPTO_SECRET.length >= 32);
  assert.notEqual(secrets.SESSION_SECRET, secrets.CRYPTO_SECRET);
  const secretPath = path.join(directory, 'secrets.env');
  const timestamp = new Date('2000-01-01T00:00:00Z');
  fs.utimesSync(secretPath, timestamp, timestamp);
  assert.deepEqual(loadOrCreateSecrets(directory), secrets);
  assert.equal(fs.statSync(secretPath).mtimeMs, timestamp.getTime());
});

test('damaged secrets are rejected and preserved for recovery', (t) => {
  const directory = temporaryDirectory(t);
  const secretPath = path.join(directory, 'secrets.env');
  for (const content of [
    'SESSION_SECRET=short\n',
    `SESSION_SECRET=${'a'.repeat(32)}\nCRYPTO_SECRET=${'a'.repeat(32)}\n`,
  ]) {
    fs.writeFileSync(secretPath, content);
    assert.throws(() => loadOrCreateSecrets(directory), /Invalid server secrets/);
    assert.equal(fs.readFileSync(secretPath, 'utf8'), content);
  }
});

test('read errors cannot fall back to newly generated secrets', (t) => {
  const directory = temporaryDirectory(t);
  t.mock.method(fs, 'readFileSync', () => {
    throw Object.assign(new Error('Access denied'), { code: 'EACCES' });
  });
  assert.throws(() => loadOrCreateSecrets(directory), { code: 'EACCES' });
  assert.equal(fs.existsSync(path.join(directory, 'secrets.env')), false);
});

test('write errors prevent startup with unpersisted secrets', (t) => {
  const directory = temporaryDirectory(t);
  t.mock.method(fs, 'writeFileSync', () => {
    throw Object.assign(new Error('Disk full'), { code: 'ENOSPC' });
  });
  assert.throws(() => loadOrCreateSecrets(directory), { code: 'ENOSPC' });
});

test('concurrent initialization uses the persisted winner without overwriting it', (t) => {
  const directory = temporaryDirectory(t);
  const persisted = { SESSION_SECRET: 'a'.repeat(64), CRYPTO_SECRET: 'b'.repeat(64) };
  const writeFileSync = fs.writeFileSync;
  t.mock.method(fs, 'writeFileSync', (filename, content, options) => {
    writeFileSync(filename, `SESSION_SECRET=${persisted.SESSION_SECRET}\nCRYPTO_SECRET=${persisted.CRYPTO_SECRET}\n`);
    return writeFileSync(filename, content, options);
  });
  assert.deepEqual(loadOrCreateSecrets(directory), persisted);
});
