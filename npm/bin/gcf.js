#!/usr/bin/env node
// Thin wrapper that executes the downloaded gcf binary.

const { execFileSync } = require('child_process');
const path = require('path');
const fs = require('fs');

const ext = process.platform === 'win32' ? '.exe' : '';
const binary = path.join(__dirname, `gcf${ext}`);

if (!fs.existsSync(binary)) {
  console.error('gcf binary not found. Run: npm rebuild @blackwell-systems/gcf-cli');
  process.exit(1);
}

try {
  execFileSync(binary, process.argv.slice(2), { stdio: 'inherit' });
} catch (err) {
  process.exit(err.status || 1);
}
