/**
 * RunAnywhere Device Bridge (stub).
 * Sets __RunAnywhereDeviceBridge and __RunAnywhereFetch for the Go WASM SDK.
 * Contract version for compatibility checks; stub returns empty/synthetic data.
 * Replace with a Web SDK-backed implementation for real inference.
 */

(function (globalThis) {
  const BRIDGE_VERSION = '1.0.0';
  const MIN_WEB_SDK_VERSION = '1.2.0'; // minimum when using Web SDK-backed bridge

  let nextHandle = 1;
  const simulatedErrors = globalThis.__RunAnywhereBridgeSimulatedErrors || {};
  const simulatedFetchError = globalThis.__RunAnywhereFetchSimulatedError;

  function base64ToUint8Array(base64) {
    if (!base64) {
      return undefined;
    }
    const bin = atob(base64);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i += 1) {
      out[i] = bin.charCodeAt(i);
    }
    return out;
  }

  function uint8ArrayToBase64(bytes) {
    if (!bytes || bytes.length === 0) {
      return '';
    }
    let bin = '';
    const chunkSize = 0x8000;
    for (let i = 0; i < bytes.length; i += chunkSize) {
      const chunk = bytes.subarray(i, i + chunkSize);
      bin += String.fromCharCode.apply(null, chunk);
    }
    return btoa(bin);
  }

  function getSimulatedError(method) {
    const v = simulatedErrors[method];
    if (!v) {
      return '';
    }
    return v === true ? `simulated bridge error (${method})` : String(v);
  }

  /**
   * Decodes a base64-encoded little-endian float32 vector (vector_bin) to Float32Array.
   * Matches the Go EncodeVectorBin format from device/stream_frame.go.
   */
  function decodeVectorBin(base64Str) {
    if (!base64Str) return new Float32Array(0);
    const bytes = base64ToUint8Array(base64Str);
    return new Float32Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 4);
  }

  function invokeCallback(cb, value) {
    if (typeof cb === 'function') {
      try {
        cb(typeof value === 'string' ? value : JSON.stringify(value));
      } catch (e) {
        cb(JSON.stringify({ error: String(e.message || e) }));
      }
    }
  }

  const bridge = {
    init(configJson, callback) {
      const simErr = getSimulatedError('init');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, {
        success: true,
        capabilities: ['llm', 'stt', 'tts', 'embeddings', 'streaming'],
      });
    },

    shutdown(callback) {
      const simErr = getSimulatedError('shutdown');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, '{"ok":true}');
    },

    createLLM(argsJson, callback) {
      const simErr = getSimulatedError('createLLM');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      const h = nextHandle++;
      invokeCallback(callback, { success: true, handle: h });
    },

    llmGenerate(argsJson, callback) {
      const simErr = getSimulatedError('llmGenerate');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, { success: true, text: '' });
    },

    llmGenerateStream(argsJson, onChunk) {
      const simErr = getSimulatedError('llmGenerateStream');
      if (simErr) {
        if (typeof onChunk === 'function') {
          onChunk(JSON.stringify({ error: simErr, done: true }));
        }
        return;
      }
      if (typeof onChunk === 'function') {
        onChunk(JSON.stringify({ done: true }));
      }
    },

    closeLLM(argsJson, callback) {
      const simErr = getSimulatedError('closeLLM');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, 'ok');
    },

    createSTT(argsJson, callback) {
      const simErr = getSimulatedError('createSTT');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      const h = nextHandle++;
      invokeCallback(callback, { success: true, handle: h });
    },

    sttTranscribe(argsJson, callback) {
      const simErr = getSimulatedError('sttTranscribe');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, { success: true, text: '' });
    },

    sttTranscribeStream(argsJson, onChunk) {
      const simErr = getSimulatedError('sttTranscribeStream');
      if (simErr) {
        if (typeof onChunk === 'function') {
          onChunk(JSON.stringify({ error: simErr, done: true }));
        }
        return;
      }
      if (typeof onChunk === 'function') {
        onChunk(JSON.stringify({ done: true }));
      }
    },

    closeSTT(argsJson, callback) {
      const simErr = getSimulatedError('closeSTT');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, 'ok');
    },

    createTTS(argsJson, callback) {
      const simErr = getSimulatedError('createTTS');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      const h = nextHandle++;
      invokeCallback(callback, { success: true, handle: h });
    },

    ttsSynthesize(argsJson, callback) {
      const simErr = getSimulatedError('ttsSynthesize');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, { success: true, audioData: '' });
    },

    closeTTS(argsJson, callback) {
      const simErr = getSimulatedError('closeTTS');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, 'ok');
    },

    createEmbeddings(argsJson, callback) {
      const simErr = getSimulatedError('createEmbeddings');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      const h = nextHandle++;
      invokeCallback(callback, { success: true, handle: h });
    },

    embeddingsEmbed(argsJson, callback) {
      const simErr = getSimulatedError('embeddingsEmbed');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, { success: true, embedding: [], vector_bin: '' });
    },

    embeddingsEmbedBatch(argsJson, callback) {
      const simErr = getSimulatedError('embeddingsEmbedBatch');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, { success: true, embeddings: [], dimension: 0 });
    },

    closeEmbeddings(argsJson, callback) {
      const simErr = getSimulatedError('closeEmbeddings');
      if (simErr) {
        invokeCallback(callback, { success: false, error: simErr });
        return;
      }
      invokeCallback(callback, 'ok');
    },
  };

  bridge.version = BRIDGE_VERSION;
  bridge.minWebSdkVersion = MIN_WEB_SDK_VERSION;
  bridge.decodeVectorBin = decodeVectorBin;
  globalThis.__RunAnywhereDeviceBridge = bridge;

  // __RunAnywhereFetch: (url, optsJson, callback) for WASM HTTP client.
  // optsJson: { method, headers (JSON string), body (base64) }.
  // callback(statusCode, headersJson, bodyBase64, errorMsg).
  // Returns { abort() } to allow cancellation from Go.
  if (typeof fetch !== 'undefined') {
    globalThis.__RunAnywhereFetch = function (url, optsJson, callback) {
      if (simulatedFetchError) {
        callback(-1, '{}', '', String(simulatedFetchError));
        return { abort: function () {} };
      }
      let opts;
      try {
        opts = typeof optsJson === 'string' ? JSON.parse(optsJson) : optsJson;
      } catch (e) {
        callback(-1, '{}', '', 'invalid opts JSON');
        return { abort: function () {} };
      }
      const controller = typeof AbortController !== 'undefined' ? new AbortController() : null;
      const headers = typeof opts.headers === 'string' ? (opts.headers ? JSON.parse(opts.headers) : {}) : opts.headers || {};
      const bodyBytes = base64ToUint8Array(opts.body);
      const body = bodyBytes ? bodyBytes.buffer : undefined;
      fetch(url, { method: opts.method || 'GET', headers, body, signal: controller ? controller.signal : undefined })
        .then(function (r) {
          return r.arrayBuffer().then(function (ab) {
            const bodyBase64 = uint8ArrayToBase64(new Uint8Array(ab));
            const h = {};
            r.headers.forEach(function (v, k) { h[k] = v; });
            callback(r.status, JSON.stringify(h), bodyBase64);
          });
        })
        .catch(function (e) {
          callback(-1, '{}', '', String(e.message || e));
        });
      return {
        abort: function () {
          if (controller) {
            controller.abort();
          }
        },
      };
    };
  } else {
    globalThis.__RunAnywhereFetch = function (url, optsJson, callback) {
      callback(-1, '{}', '', 'fetch not available');
      return { abort: function () {} };
    };
  }
})(typeof globalThis !== 'undefined' ? globalThis : typeof window !== 'undefined' ? window : self);
