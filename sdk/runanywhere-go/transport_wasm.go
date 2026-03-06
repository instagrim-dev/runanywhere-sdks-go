//go:build js && wasm

package runanywhere

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"syscall/js"
)

// DefaultWASMTransport returns an http.RoundTripper that uses the browser's fetch API
// via the global __RunAnywhereFetch. Use when creating a Client in WASM:
//
//	client := runanywhere.NewClient(baseURL, runanywhere.WithHTTPClient(&http.Client{
//	    Transport: runanywhere.DefaultWASMTransport(),
//	}))
//
// The host or bridge must define __RunAnywhereFetch(url, options, callback) where
// options is { method, headers: {...}, body: base64String } and callback(statusCode, headersJson, bodyBase64, errorMsg) is called when the fetch completes.
// The function may return an object with abort() to support cancellation.
func DefaultWASMTransport() http.RoundTripper {
	return &wasmTransport{}
}

type wasmTransport struct{}

func (t *wasmTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fetch := js.Global().Get("__RunAnywhereFetch")
	if fetch.IsUndefined() {
		return nil, &transportError{msg: "__RunAnywhereFetch is not defined"}
	}

	type result struct {
		resp *http.Response
		err  error
	}
	ch := make(chan result, 1)

	bodyBase64 := ""
	if req.Body != nil {
		slurp, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(slurp))
		bodyBase64 = base64.StdEncoding.EncodeToString(slurp)
	}

	headers := make(map[string]string)
	for k := range req.Header {
		headers[k] = req.Header.Get(k)
	}
	headersJSONBytes, err := json.Marshal(headers)
	if err != nil {
		return nil, &transportError{msg: "failed to marshal headers: " + err.Error()}
	}
	headersJSON := string(headersJSONBytes)

	opts := map[string]any{
		"method":  req.Method,
		"headers": headersJSON,
		"body":    bodyBase64,
	}
	optsJSONBytes, err := json.Marshal(opts)
	if err != nil {
		return nil, &transportError{msg: "failed to marshal fetch options: " + err.Error()}
	}
	optsJSON := string(optsJSONBytes)

	var callback js.Func
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			callback.Release()
		})
	}
	callback = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer release()
		if len(args) < 3 {
			select {
			case ch <- result{err: &transportError{msg: "fetch callback: missing args"}}:
			default:
			}
			return nil
		}
		status := args[0].Int()
		headersStr := args[1].String()
		bodyB64 := args[2].String()
		errMsg := ""
		if len(args) > 3 && !args[3].IsUndefined() {
			errMsg = args[3].String()
		}
		if errMsg != "" {
			select {
			case ch <- result{err: &transportError{msg: errMsg}}:
			default:
			}
			return nil
		}
		var bodyBytes []byte
		if bodyB64 != "" {
			decoded, decErr := base64.StdEncoding.DecodeString(bodyB64)
			if decErr != nil {
				select {
				case ch <- result{err: &transportError{msg: "invalid base64 response body: " + decErr.Error()}}:
				default:
				}
				return nil
			}
			bodyBytes = decoded
		}
		resp := &http.Response{
			StatusCode: status,
			Header:     parseHeadersJSON(headersStr),
			Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
			Request:    req,
		}
		select {
		case ch <- result{resp: resp}:
		default:
		}
		return nil
	})

	fetchReq := fetch.Invoke(req.URL.String(), js.ValueOf(optsJSON), callback)
	abort := js.Undefined()
	if fetchReq.Type() == js.TypeObject {
		abort = fetchReq.Get("abort")
	}

	select {
	case r := <-ch:
		return r.resp, r.err
	case <-req.Context().Done():
		if abort.Type() == js.TypeFunction {
			abort.Invoke()
		}
		return nil, req.Context().Err()
	}
}

func parseHeadersJSON(s string) http.Header {
	h := http.Header{}
	if s == "" || s == "{}" {
		return h
	}
	var m map[string]string
	_ = json.Unmarshal([]byte(s), &m)
	for k, v := range m {
		h.Set(k, v)
	}
	return h
}

type transportError struct{ msg string }

func (e *transportError) Error() string { return e.msg }
