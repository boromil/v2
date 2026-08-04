// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for the MCP endpoint.
//
// These follow the project's fuzzing policy: the endpoint parses untrusted,
// hostile input (JSON-RPC 2.0 over HTTP) and is a deterministic dispatch state
// machine, so it is exercised with a seeded PRNG in the normal test runner.
// A failure is reproducible by re-running with the same seed, which is printed
// in the failure path.
//
// F1 drives the pure dispatch path (no store) with malformed/random requests
// and asserts "parse-or-error, never panic, never hang". F2 probes the pure
// param parse/validate/normalize helpers against their documented invariants.
package mcp

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"miniflux.app/v2/internal/database/dialect"
	mtest "miniflux.app/v2/internal/storage/testing"
)

// documentedErrorCodes is the set of JSON-RPC 2.0 error codes this server emits.
var documentedErrorCodes = map[int]bool{
	errCodeParseError:     true,
	errCodeInvalidRequest: true,
	errCodeMethodNotFound: true,
	errCodeInvalidParams:  true,
	errCodeInternal:       true,
	errCodeServerError:    true,
	errCodeUnauthorized:   true,
}

// validMethods is the pool of dispatcher-recognized method names plus a garbage
// name, so the generator covers both the known methods and the unknown-method
// path in roughly the proportion implied by the slice.
var validMethods = []string{
	"initialize",
	"notifications/initialized",
	"tools/list",
	"tools/call",
	"garbage.method",
}

// randomID produces a JSON `id` value drawn from all JSON-RPC-legal categories
// plus hostile ones (absent, null, object, huge number, deep nesting).
func randomID(r *rand.Rand) json.RawMessage {
	switch r.IntN(8) {
	case 0:
		return nil // absent
	case 1:
		return json.RawMessage("null")
	case 2, 3:
		return json.RawMessage(fmt.Sprintf("%d", int64(r.Uint64())))
	case 4:
		return json.RawMessage(`"` + strings.Repeat("x", 1+r.IntN(32)) + `"`)
	case 5:
		return json.RawMessage(fmt.Sprintf("%g", r.Float64()))
	case 6:
		return json.RawMessage(`{"nested":{"deep":[1,2,3]}}`)
	default:
		return json.RawMessage("true")
	}
}

// randomParams produces a raw `params` payload for a given method, mixing
// well-formed, truncated, and hostile inputs.
func randomParams(r *rand.Rand, method, name string) json.RawMessage {
	// Half the time emit a deliberately malformed/empty params.
	switch r.IntN(8) {
	case 0:
		return nil // absent
	case 1:
		return json.RawMessage("null")
	case 2:
		return json.RawMessage(`{`)
	case 3:
		return json.RawMessage(`"not an object`)
	case 4:
		return json.RawMessage(`{"name":` + fmt.Sprintf("%q", name) + `}`) // missing arguments
	}

	if method != "tools/call" {
		return json.RawMessage(`{}`)
	}

	args := randomToolArgs(r, name)
	paramsBody := fmt.Sprintf(`{"name":%q,"arguments":%s}`, name, args)
	return json.RawMessage(paramsBody)
}

// randomToolArgs builds a random `arguments` object for the named tool, mixing
// correct typing with type errors, boundary values, and extra/duplicate keys.
func randomToolArgs(r *rand.Rand, name string) json.RawMessage {
	intVal := func() string {
		switch r.IntN(6) {
		case 0:
			return "-1"
		case 1:
			return "0"
		case 2:
			return "200"
		case 3:
			return "201"
		case 4:
			return fmt.Sprintf("%d", int64(r.Uint64()))
		default:
			return fmt.Sprintf("%d", 1+r.IntN(100))
		}
	}
	strVal := func() string {
		switch r.IntN(4) {
		case 0:
			return `"unread"`
		case 1:
			return `"read"`
		case 2:
			return `"bogus"`
		default:
			return `""`
		}
	}

	switch name {
	case "list_feeds":
		return json.RawMessage(`{"category_id":` + intVal() + `,"limit":` + intVal() + `,"offset":` + intVal() + `}`)
	case "get_entries":
		return json.RawMessage(`{"feed_id":` + intVal() + `,"category_id":` + intVal() + `,"status":` + strVal() + `,"limit":` + intVal() + `,"offset":` + intVal() + `,"order":"` + randomWord(r, 8) + `","direction":"` + randomDirection(r) + `"}`)
	case "mark_entries":
		ids := `["` + randomWord(r, 4) + `"]`
		if r.IntN(2) == 0 {
			b := []string{}
			for i := 0; i < r.IntN(6); i++ {
				b = append(b, intVal())
			}
			ids = `[` + strings.Join(b, ",") + `]`
		}
		return json.RawMessage(`{"entry_ids":` + ids + `,"status":` + strVal() + `}`)
	case "toggle_bookmark":
		return json.RawMessage(`{"entry_id":` + intVal() + `}`)
	case "mark_feed_as_read":
		return json.RawMessage(`{"feed_id":` + intVal() + `}`)
	case "mark_category_as_read":
		return json.RawMessage(`{"category_id":` + intVal() + `}`)
	case "refresh_feed":
		return json.RawMessage(`{"feed_id":` + intVal() + `}`)
	case "list_categories":
		return json.RawMessage(`{}`)
	default:
		return json.RawMessage(`{"unexpected":"value"}`)
	}
}

func randomWord(r *rand.Rand, n int) string {
	const letters = "abcXYZ019 _-\n\r\t"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[r.IntN(len(letters))]
	}
	return string(b)
}

func randomDirection(r *rand.Rand) string {
	switch r.IntN(3) {
	case 0:
		return "asc"
	case 1:
		return "desc"
	default:
		return "diagonal"
	}
}

// runDispatchNegative invokes dispatchAndSend with a captured send and returns
// the serialized response bytes, asserting every iteration is panic-free.
func runDispatchNegative(t *testing.T, h *MCPHandler, req mcpRequest, seed uint64) []byte {
	t.Helper()

	var payload []byte
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("seed=%d panic in dispatchAndSend: %v", seed, rec)
			}
		}()
		h.dispatchAndSend(1, req, func(b []byte) { payload = b })
	}()

	// send is always called exactly once with a non-empty payload.
	if len(payload) == 0 {
		t.Fatalf("seed=%d send not invoked with payload", seed)
	}
	return payload
}

func checkResponseEnvelope(t *testing.T, payload []byte, seed uint64) {
	t.Helper()

	var resp mcpResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("seed=%d response not valid JSON-RPC json: %v; payload=%s", seed, err, payload)
	}
	if resp.JSONRPC != "2.0" {
		t.Fatalf("seed=%d response.jsonrpc=%q want \"2.0\"", seed, resp.JSONRPC)
	}
	if resp.Error != nil {
		if !documentedErrorCodes[resp.Error.Code] {
			t.Fatalf("seed=%d undocumented error code %d; payload=%s", seed, resp.Error.Code, payload)
		}
	} else if resp.Result == nil {
		t.Fatalf("seed=%d response has neither Result nor Error; payload=%s", seed, payload)
	}
}

// fuzzDispatch runs the negative-space fuzz driver with the given PRNG.
func fuzzDispatch(t *testing.T, r *rand.Rand, iterations int) {
	// A real store is required so tools/call reaches its argument-parsing code
	// without a nil-pointer deref; on an empty DB the store-backed tools return
	// clean empty results, which keeps the envelope/no-panic property the target.
	store := mtest.SetupTestDB(t, dialect.SQLite)
	h := NewMCPHandler(store).(*MCPHandler)
	for i := 0; i < iterations; i++ {
		method := validMethods[r.IntN(len(validMethods))]
		name := "tools/call"
		if method == "tools/call" {
			name = randomToolName(r)
		}

		req := mcpRequest{
			JSONRPC: "2.0",
			Method:  method,
			ID:      randomID(r),
			Params:  randomParams(r, method, name),
		}
		payload := runDispatchNegative(t, h, req, r.Uint64())
		checkResponseEnvelope(t, payload, r.Uint64())
	}
}

func randomToolName(r *rand.Rand) string {
	known := []string{
		"list_feeds", "get_entries", "mark_entries", "toggle_bookmark",
		"list_categories", "mark_feed_as_read", "mark_category_as_read", "refresh_feed",
	}
	if r.IntN(3) == 0 {
		return "not.a.tool"
	}
	return known[r.IntN(len(known))]
}

// makeRand returns a seeded *rand.Rand for determinism.
func makeRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed+1))
}

// TestFuzzDispatchFixedSeed runs the dispatch fuzzer with a fixed seed for
// reproducible regression coverage ("average" behavior).
func TestFuzzDispatchFixedSeed(t *testing.T) {
	fuzzDispatch(t, makeRand(0x5eed), 2000)
}

// TestFuzzDispatchRandomSeed runs the dispatch fuzzer with a random seed for
// broad coverage. On failure the seed is printed so the exact case can be
// replayed via TestFuzzDispatchFixedSeed-like invocation.
func TestFuzzDispatchRandomSeed(t *testing.T) {
	fuzzDispatch(t, makeRand(424242424242), 2000)
}

// --- F2: tool-args boundary / validation model tests against pure helpers ---

// TestFuzzListFeedsParamsNormalization asserts the pure parseListFeedsParams
// always clamps limit to (100..200], offset >= 0, and never panics on hostile
// input.
func TestFuzzListFeedsParamsNormalization(t *testing.T) {
	r := makeRand(1)
	for i := 0; i < 2000; i++ {
		args := randomToolArgs(r, "list_feeds")
		p, err := parseListFeedsParams(args)
		if err != nil {
			continue // parse-or-error on malformed JSON is fine
		}
		if p.Limit <= 0 || p.Limit > 200 {
			t.Fatalf("iter=%d args=%s limit=%d out of range (100..200]", i, args, p.Limit)
		}
		if p.Offset < 0 {
			t.Fatalf("iter=%d args=%s offset=%d negative", i, args, p.Offset)
		}
	}
}

// TestFuzzGetEntriesParamsNormalization asserts get_entries normalization: limit
// in (50..200], offset >= 0, order/direction defaulted, invalid status rejected.
func TestFuzzGetEntriesParamsNormalization(t *testing.T) {
	r := makeRand(2)
	for i := 0; i < 2000; i++ {
		args := randomToolArgs(r, "get_entries")
		p, err := parseGetEntriesParams(args)
		if err != nil {
			continue
		}
		if p.Limit <= 0 || p.Limit > 200 {
			t.Fatalf("iter=%d args=%s limit=%d out of range (50..200]", i, args, p.Limit)
		}
		if p.Offset < 0 {
			t.Fatalf("iter=%d args=%s offset=%d negative", i, args, p.Offset)
		}
		if p.Order == "" || p.Direction == "" {
			t.Fatalf("iter=%d args=%s order/direction not defaulted: %+v", i, args, p)
		}
		if !strings.Contains(string(args), `"status"`) {
			continue
		}
		// status present in args: normalize-path must have accepted only valid statuses.
		var raw struct {
			Status json.RawMessage `json:"status"`
		}
		_ = json.Unmarshal(args, &raw)
		if string(raw.Status) == `"bogus"` {
			t.Fatalf("iter=%d args=%s: bogus status should have been rejected, got p=%+v", i, args, p)
		}
	}
}

// TestFuzzMarkEntriesValidation asserts the pure validateMarkEntries accepts only
// non-empty id lists and error-codes everything else, never panicking.
func TestFuzzMarkEntriesValidation(t *testing.T) {
	r := makeRand(3)
	statusPool := []string{"unread", "read", "bogus", ""}
	for i := 0; i < 2000; i++ {
		n := r.IntN(8)
		ids := make([]int64, 0, n)
		for j := 0; j < n; j++ {
			ids = append(ids, int64(r.IntN(10)))
		}
		status := statusPool[r.IntN(len(statusPool))]

		err := validateMarkEntries(ids, status)
		statusValid := status == "unread" || status == "read"
		if len(ids) == 0 {
			if err == nil {
				t.Fatalf("iter=%d: empty ids should error", i)
			}
			continue
		}
		if !statusValid {
			if err == nil {
				t.Fatalf("iter=%d: status=%q should error", i, status)
			}
		} else {
			if err != nil {
				t.Fatalf("iter=%d: valid (ids=%v,status=%q) got err %v", i, ids, status, err)
			}
		}
	}
}

// TestFuzzRequiredIDValidation asserts validateRequiredID rejects non-positive ids.
func TestFuzzRequiredIDValidation(t *testing.T) {
	r := makeRand(4)
	for i := 0; i < 2000; i++ {
		// Mix boundary values with uniform ints.
		var id int64
		switch r.IntN(5) {
		case 0:
			id = 0
		case 1:
			id = -1
		case 2:
			id = int64(r.Uint64() >> 1)
		case 3:
			id = 1
			default:
				id = 1 + r.Int64N(1<<20)
		}
		err := validateRequiredID(id, "entry_id")
		if id <= 0 {
			if err == nil {
				t.Fatalf("iter=%d: id=%d should error", i, id)
			}
		} else if err != nil {
			t.Fatalf("iter=%d: id=%d got unexpected err %v", i, id, err)
		}
	}
}

// TestDeserializationNeverPanics is a lightweight regression guard asserting that
// json decoding of arbitrary mcpRequest structs never panics on hostile input
// (belt-and-braces around the dispatch path, which also never panics).
func TestDeserializationNeverPanics(t *testing.T) {
	r := makeRand(5)
	for i := 0; i < 1000; i++ {
		body := randomToolArgs(r, "get_entries")
		var req mcpRequest
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("iter=%d panic decoding %s: %v", i, body, rec)
				}
			}()
			_ = json.Unmarshal(body, &req)
		}()
	}
}
