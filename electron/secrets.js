const crypto = require('crypto');
const fs = require('fs');
const path = require('path');

// Never rotate an existing key implicitly: unreadable or damaged storage must
// stop startup instead of invalidating sessions and changing derived values.
function loadOrCreateSecrets(userDataPath) {
  const secretsPath = path.join(userDataPath, 'secrets.env');
  let content;
  try {
    content = fs.readFileSync(secretsPath, 'utf8');
  } catch (err) {
    if (err.code !== 'ENOENT') throw err;

    const secrets = {
      SESSION_SECRET: crypto.randomBytes(48).toString('base64'),
      CRYPTO_SECRET: crypto.randomBytes(48).toString('base64'),
    };
    fs.mkdirSync(userDataPath, { recursive: true });
    try {
      fs.writeFileSync(
        secretsPath,
        `SESSION_SECRET=${secrets.SESSION_SECRET}\nCRYPTO_SECRET=${secrets.CRYPTO_SECRET}\n`,
        { mode: 0o600, flag: 'wx' }
      );
    } catch (writeError) {
      // Another instance may have initialized the same user data directory.
      if (writeError.code === 'EEXIST') return loadOrCreateSecrets(userDataPath);
      throw writeError;
    }
    return secrets;
  }

  const secrets = {};
  for (const line of content.split('\n')) {
    const idx = line.indexOf('=');
    if (idx > 0) {
      const name = line.slice(0, idx).trim();
      if (name === 'SESSION_SECRET' || name === 'CRYPTO_SECRET') {
        secrets[name] = line.slice(idx + 1).trim();
      }
    }
  }
  if (
    !secrets.SESSION_SECRET || secrets.SESSION_SECRET.length < 32 ||
    !secrets.CRYPTO_SECRET || secrets.CRYPTO_SECRET.length < 32 ||
    secrets.SESSION_SECRET === secrets.CRYPTO_SECRET
  ) {
    throw new Error(`Invalid server secrets in ${secretsPath}; restore the original secrets file.`);
  }
  return secrets;
}

module.exports = { loadOrCreateSecrets };
