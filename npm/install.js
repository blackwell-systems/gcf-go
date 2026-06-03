#!/usr/bin/env node
// Downloads the prebuilt gcf binary for the current platform from GitHub Releases.

const { execSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const https = require('https');

const REPO = 'blackwell-systems/gcf-go';
const BIN_DIR = path.join(__dirname, 'bin');
const VERSION = require('./package.json').version;

function getPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  const map = {
    'linux-x64': 'linux-amd64',
    'linux-arm64': 'linux-arm64',
    'darwin-x64': 'darwin-amd64',
    'darwin-arm64': 'darwin-arm64',
    'win32-x64': 'windows-amd64.exe',
  };

  const key = `${platform}-${arch}`;
  const suffix = map[key];
  if (!suffix) {
    console.error(`Unsupported platform: ${key}`);
    console.error('Install from source: go install github.com/blackwell-systems/gcf-go/cmd/gcf@latest');
    process.exit(1);
  }
  return suffix;
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const follow = (url) => {
      https.get(url, { headers: { 'User-Agent': 'gcf-cli-npm' } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          follow(res.headers.location);
          return;
        }
        if (res.statusCode !== 200) {
          reject(new Error(`Download failed: HTTP ${res.statusCode} from ${url}`));
          return;
        }
        const file = fs.createWriteStream(dest);
        res.pipe(file);
        file.on('finish', () => { file.close(); resolve(); });
        file.on('error', reject);
      }).on('error', reject);
    };
    follow(url);
  });
}

async function main() {
  const suffix = getPlatform();
  const tag = `v${VERSION}`;
  const filename = `gcf-${suffix}`;
  const url = `https://github.com/${REPO}/releases/download/${tag}/${filename}`;

  fs.mkdirSync(BIN_DIR, { recursive: true });

  const dest = path.join(BIN_DIR, suffix.endsWith('.exe') ? 'gcf.exe' : 'gcf');

  console.log(`Downloading gcf ${tag} for ${suffix}...`);

  try {
    await download(url, dest);
    fs.chmodSync(dest, 0o755);
    console.log(`Installed gcf to ${dest}`);
  } catch (err) {
    console.error(`Failed to download: ${err.message}`);
    console.error(`URL: ${url}`);
    console.error('Install from source: go install github.com/blackwell-systems/gcf-go/cmd/gcf@latest');
    process.exit(1);
  }
}

main();
