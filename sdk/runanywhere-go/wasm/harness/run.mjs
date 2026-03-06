#!/usr/bin/env node
/**
 * Minimal WASM harness: load bridge, wasm_exec, run runanywhere.wasm, then call deviceInit.
 * Run from sdk/runanywhere-go: node wasm/harness/run.mjs
 * Requires dist/runanywhere.wasm and dist/wasm_exec.js (copy from GOROOT/lib/wasm/wasm_exec.js).
 */
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { createRequire } from 'module';
import vm from 'vm';

const require = createRequire(import.meta.url);
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '../..');
const distDir = path.join(repoRoot, 'dist');
const bridgePath = path.join(repoRoot, 'wasm', 'bridge', 'bridge.js');
const wasmPath = path.join(distDir, 'runanywhere.wasm');

const goRoot = process.env.GOROOT || '';
const wasmExecFromGo = goRoot ? path.join(goRoot, 'lib', 'wasm', 'wasm_exec.js') : '';
const wasmExecPath = (wasmExecFromGo && fs.existsSync(wasmExecFromGo)) ? wasmExecFromGo : path.join(distDir, 'wasm_exec.js');

if (!fs.existsSync(bridgePath)) {
  console.error('Bridge not found:', bridgePath);
  process.exit(1);
}
if (!fs.existsSync(wasmExecPath)) {
  console.error('wasm_exec.js not found. Copy: cp $(go env GOROOT)/lib/wasm/wasm_exec.js', distDir);
  process.exit(1);
}
if (!fs.existsSync(wasmPath)) {
  console.error('runanywhere.wasm not found. Build: GOOS=js GOARCH=wasm go build -o dist/runanywhere.wasm ./cmd/runanywhere-wasm');
  process.exit(1);
}

// Node globals required by wasm_exec.js
globalThis.require = require;
globalThis.fs = require('fs');
globalThis.path = require('path');
globalThis.process = process;
globalThis.TextEncoder = require('util').TextEncoder;
globalThis.TextDecoder = require('util').TextDecoder;
globalThis.performance = require('perf_hooks').performance;
// globalThis.crypto is already set by Node

// 1. Load bridge (sets globalThis.__RunAnywhereDeviceBridge)
vm.runInThisContext(fs.readFileSync(bridgePath, 'utf8'), { filename: bridgePath });

// 2. Load wasm_exec.js (defines globalThis.Go)
require(wasmExecPath);

// 3. Instantiate and run WASM
const buf = fs.readFileSync(wasmPath);
const go = new Go();
const result = await WebAssembly.instantiate(buf, go.importObject);
go.run(result.instance);

// 4. After Go main registers exports and blocks, check readiness and call deviceInit.
// Prefer callback mode so exports do not block the JS event loop.
setImmediate(async () => {
  if (!globalThis.__RunAnywhereWasmReady) {
    console.error('FAIL: __RunAnywhereWasmReady not true');
    process.exit(1);
  }
  const initResult = await new Promise((resolve) => {
    globalThis.deviceInit('{}', resolve);
  });
  const parsed = typeof initResult === 'string' ? JSON.parse(initResult) : initResult;
  if (parsed.error) {
    console.error('FAIL: deviceInit error', parsed.error);
    process.exit(1);
  }
  console.log('OK: deviceInit succeeded');
  process.exit(0);
});
