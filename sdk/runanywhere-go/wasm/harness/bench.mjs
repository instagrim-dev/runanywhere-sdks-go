#!/usr/bin/env node
/**
 * Browser WASM performance benchmark harness.
 * Reuses the same bootstrap pattern as run.mjs (load bridge → wasm_exec.js → instantiate WASM).
 * Exercises each device export with performance.now() timing.
 *
 * Run from sdk/runanywhere-go:
 *   GOOS=js GOARCH=wasm go build -o dist/runanywhere.wasm ./cmd/runanywhere-wasm
 *   cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" dist/
 *   node wasm/harness/bench.mjs --iterations 100
 */
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { createRequire } from 'module';
import vm from 'vm';
import { performance } from 'perf_hooks';

const require = createRequire(import.meta.url);
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '../..');
const distDir = path.join(repoRoot, 'dist');
const bridgePath = path.join(repoRoot, 'wasm', 'bridge', 'bridge.js');
const wasmPath = path.join(distDir, 'runanywhere.wasm');

// Parse CLI flags
const args = process.argv.slice(2);
let iterations = 1000;
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--iterations' && args[i + 1]) {
    iterations = parseInt(args[i + 1], 10);
    i++;
  }
}

const WARMUP = 10;

// =============================================================================
// Bootstrap (same as run.mjs)
// =============================================================================

const goRoot = process.env.GOROOT || '';
const wasmExecFromGo = goRoot ? path.join(goRoot, 'lib', 'wasm', 'wasm_exec.js') : '';
const wasmExecPath = (wasmExecFromGo && fs.existsSync(wasmExecFromGo))
  ? wasmExecFromGo
  : path.join(distDir, 'wasm_exec.js');

for (const [label, p] of [['Bridge', bridgePath], ['wasm_exec.js', wasmExecPath], ['runanywhere.wasm', wasmPath]]) {
  if (!fs.existsSync(p)) {
    console.error(`${label} not found: ${p}`);
    process.exit(1);
  }
}

// Node globals required by wasm_exec.js
globalThis.require = require;
globalThis.fs = require('fs');
globalThis.path = require('path');
globalThis.process = process;
globalThis.TextEncoder = require('util').TextEncoder;
globalThis.TextDecoder = require('util').TextDecoder;
globalThis.performance = performance;

// Load bridge (sets globalThis.__RunAnywhereDeviceBridge)
vm.runInThisContext(fs.readFileSync(bridgePath, 'utf8'), { filename: bridgePath });

// Load wasm_exec.js (defines globalThis.Go)
require(wasmExecPath);

// Instantiate and run WASM
const buf = fs.readFileSync(wasmPath);
const go = new Go();
const result = await WebAssembly.instantiate(buf, go.importObject);
go.run(result.instance);

// =============================================================================
// Benchmark Utilities
// =============================================================================

/**
 * Wraps a callback-style device export into a Promise.
 * @param {string} fnName - Name of the global function to call.
 * @param {...any} fnArgs - Arguments to pass (callback is appended automatically).
 * @returns {Promise<string>} The JSON result string.
 */
function callAsync(fnName, ...fnArgs) {
  return new Promise((resolve, reject) => {
    const fn = globalThis[fnName];
    if (typeof fn !== 'function') {
      reject(new Error(`global function ${fnName} not found`));
      return;
    }
    fn(...fnArgs, (result) => {
      resolve(result);
    });
  });
}

/**
 * Runs a benchmark function N times after warmup, returns timing stats.
 * @param {string} name - Benchmark name.
 * @param {number} n - Number of timed iterations.
 * @param {function} fn - Async function to benchmark.
 */
async function bench(name, n, fn) {
  // Warmup
  for (let i = 0; i < WARMUP; i++) {
    await fn();
  }

  // Timed run
  const start = performance.now();
  for (let i = 0; i < n; i++) {
    await fn();
  }
  const elapsed = performance.now() - start;
  const nsPerOp = Math.round((elapsed * 1e6) / n); // ms → ns

  // Output in Go bench format
  console.log(`Benchmark${name}\t${n}\t${nsPerOp} ns/op`);
}

// =============================================================================
// Benchmarks
// =============================================================================

setImmediate(async () => {
  if (!globalThis.__RunAnywhereWasmReady) {
    console.error('FAIL: __RunAnywhereWasmReady not true');
    process.exit(1);
  }

  // Init device
  const initResult = await callAsync('deviceInit', '{}');
  const parsed = typeof initResult === 'string' ? JSON.parse(initResult) : initResult;
  if (parsed.error) {
    console.error('FAIL: deviceInit error', parsed.error);
    process.exit(1);
  }

  console.log(`# Browser WASM benchmarks (iterations=${iterations})`);
  console.log('');

  // --- LLM lifecycle ---
  await bench('LLMLifecycle', iterations, async () => {
    const createRes = JSON.parse(await callAsync('deviceNewLLM', '/models/test.gguf'));
    if (createRes.error) throw new Error(createRes.error);
    const h = createRes.handle;
    await callAsync('deviceCloseLLM', h);
  });

  // --- LLM generate (steady-state) ---
  {
    const createRes = JSON.parse(await callAsync('deviceNewLLM', '/models/test.gguf'));
    const h = createRes.handle;
    await bench('LLMGenerate', iterations, async () => {
      await callAsync('deviceLLMGenerate', h, 'Hello world');
    });
    await callAsync('deviceCloseLLM', h);
  }

  // --- LLM streaming throughput (StreamFrame format: payload.text, done) ---
  {
    const createRes = JSON.parse(await callAsync('deviceNewLLM', '/models/test.gguf'));
    const h = createRes.handle;
    await bench('LLMGenerateStream', iterations, async () => {
      await new Promise((resolve) => {
        globalThis.deviceLLMGenerateStream(h, 'stream test', '{}', (chunk) => {
          const c = JSON.parse(chunk);
          if (c.done) resolve();
        });
      });
    });
    await callAsync('deviceCloseLLM', h);
  }

  // --- STT lifecycle + transcribe ---
  await bench('STTTranscribe', iterations, async () => {
    const createRes = JSON.parse(await callAsync('deviceNewSTT', '/models/whisper.bin'));
    if (createRes.error) throw new Error(createRes.error);
    const h = createRes.handle;
    await callAsync('deviceSTTTranscribe', h, 'SGVsbG8='); // base64("Hello")
    await callAsync('deviceCloseSTT', h);
  });

  // --- TTS lifecycle + synthesize ---
  await bench('TTSSynthesize', iterations, async () => {
    const createRes = JSON.parse(await callAsync('deviceNewTTS', '/voices/default'));
    if (createRes.error) throw new Error(createRes.error);
    const h = createRes.handle;
    await callAsync('deviceTTSynthesize', h, 'Hello world');
    await callAsync('deviceCloseTTS', h);
  });

  // --- Embeddings lifecycle + embed ---
  await bench('EmbeddingsEmbed', iterations, async () => {
    const createRes = JSON.parse(await callAsync('deviceNewEmbeddings', '/models/embed'));
    if (createRes.error) throw new Error(createRes.error);
    const h = createRes.handle;
    await callAsync('deviceEmbeddingsEmbed', h, 'Hello');
    await callAsync('deviceCloseEmbeddings', h);
  });

  console.log('');
  console.log('ok');
  process.exit(0);
});
