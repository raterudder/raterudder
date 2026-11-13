package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raterudder/raterudder/pkg/common"
	"github.com/raterudder/raterudder/pkg/feats"
	"github.com/raterudder/raterudder/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleMCP(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)

	fixedNow := time.Date(2026, 6, 15, 14, 30, 0, 0, loc)

	// meta2026 is the SEP-2575 per-request _meta a 2026-07-28 client sends in params.
	const meta2026 = `{
		"io.modelcontextprotocol/protocolVersion": "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": {},
		"io.modelcontextprotocol/clientInfo": {"name": "test-client", "version": "1.0"}
	}`

	type rpcError struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	type rpcErrorResp struct {
		Error *rpcError `json:"error"`
	}
	type toolContent struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type toolResult struct {
		ResultType string        `json:"resultType"`
		Content    []toolContent `json:"content"`
		IsError    bool          `json:"isError"`
	}

	setupMCPTestServer := func(t *testing.T) (*Server, http.Handler, string, *mockStorage) {
		mockStore := &mockStorage{}
		srv := newTestAppsServer(mockStore, fixedNow)
		rawToken, tokenHash, err := srv.createAppToken("user-1", "site-1", "app-1")
		require.NoError(t, err)

		mockStore.On("GetUser", mock.Anything, "user-1").Return(types.User{
			ID:    "user-1",
			Email: "user1@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
			Apps: []types.UserApp{
				{
					ID:        "app-1",
					Name:      "Claude Desktop",
					Type:      types.AppTypeAPIKey,
					SiteID:    "site-1",
					TokenHash: tokenHash,
				},
			},
		}, nil).Maybe()

		mockStore.On("GetSite", mock.Anything, "site-1").Return(types.Site{
			ID:          "site-1",
			Permissions: []types.SitePermissions{{UserID: "user-1"}},
		}, nil).Maybe()

		mockStore.On("GetFeatureConfig", mock.Anything, mock.Anything).Return(feats.Config{}, time.Time{}, nil).Maybe()
		mockStore.On("GetHistorySummaries", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]types.HistorySummary{}, nil).Maybe()

		return srv, srv.setupHandler(), rawToken, mockStore
	}

	// newReq builds an authenticated MCP Streamable HTTP POST /mcp request with standard Content-Type
	// and Accept headers. Pass "" in headers to delete a default header.
	newReq := func(token, body string, headers map[string]string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		for k, v := range headers {
			if v == "" {
				req.Header.Del(k)
			} else {
				req.Header.Set(k, v)
			}
		}
		return req
	}

	// headers2026 returns the SEP-2575/SEP-2243 headers required for a 2026-07-28 request.
	headers2026 := func(method, name string) map[string]string {
		h := map[string]string{
			"MCP-Protocol-Version": "2026-07-28",
			"Mcp-Method":           method,
		}
		if name != "" {
			h["Mcp-Name"] = name
		}
		return h
	}

	decodeRPCError := func(t *testing.T, w *httptest.ResponseRecorder) *rpcError {
		var resp rpcErrorResp
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp), w.Body.String())
		require.NotNil(t, resp.Error)
		return resp.Error
	}

	t.Run("AppsNotEnabled", func(t *testing.T) {
		mockStore := &mockStorage{}
		srv := newTestAppsServer(mockStore, fixedNow)
		srv.appsEncryptionKey = ""
		handler := srv.setupHandler()

		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc": "2.0", "id": 1, "method": "ping"}`))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotImplemented, w.Code)
		assert.Contains(t, w.Body.String(), "apps not enabled")
	})

	t.Run("UnauthorizedAndRevokedToken", func(t *testing.T) {
		_, handler, _, mockStore := setupMCPTestServer(t)

		// 1. Unauthenticated POST and GET both return 401 with WWW-Authenticate for OAuth discovery
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			reqNoAuth := httptest.NewRequest(method, "/mcp", nil)
			wNoAuth := httptest.NewRecorder()
			handler.ServeHTTP(wNoAuth, reqNoAuth)
			assert.Equal(t, http.StatusUnauthorized, wNoAuth.Code, "method %s should return 401 when unauthenticated", method)
			assert.Contains(t, wNoAuth.Header().Get("WWW-Authenticate"), `resource_metadata="`)
		}

		// 2. Invalid app token format/signature
		reqInvalid := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc": "2.0", "id": 1, "method": "ping"}`))
		reqInvalid.Header.Set("Authorization", "Bearer rr_invalid_app_token_value")
		wInvalid := httptest.NewRecorder()
		handler.ServeHTTP(wInvalid, reqInvalid)
		assert.Equal(t, http.StatusUnauthorized, wInvalid.Code)
		assert.Contains(t, wInvalid.Header().Get("WWW-Authenticate"), `resource_metadata="`)

		// 3. Revoked token (valid format, but app not in user.Apps)
		srvRevoked := newTestAppsServer(mockStore, fixedNow)
		revokedToken, _, err := srvRevoked.createAppToken("user-revoked", "site-1", "app-revoked")
		require.NoError(t, err)

		mockStore.On("GetUser", mock.Anything, "user-revoked").Return(types.User{
			ID:    "user-revoked",
			Email: "revoked@example.com",
			Sites: []types.UserSite{{ID: "site-1"}},
			Apps:  []types.UserApp{},
		}, nil).Once()

		reqRevoked := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc": "2.0", "id": 1, "method": "ping"}`))
		reqRevoked.Header.Set("Authorization", "Bearer "+revokedToken)
		wRevoked := httptest.NewRecorder()
		srvRevoked.setupHandler().ServeHTTP(wRevoked, reqRevoked)
		assert.Equal(t, http.StatusUnauthorized, wRevoked.Code)
		assert.Contains(t, wRevoked.Header().Get("WWW-Authenticate"), `resource_metadata="`)

		// 4. Valid browser session cookie is rejected on /mcp (must use Bearer app token, prevents CSRF)
		srvCookie, cookieHandler, _, _ := setupMCPTestServer(t)
		sessionTok, err := srvCookie.createSessionToken("user-1", "user1@example.com", "", time.Hour)
		require.NoError(t, err)
		reqSessionCookie := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}`))
		reqSessionCookie.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: sessionTok})
		wSessionCookie := httptest.NewRecorder()
		cookieHandler.ServeHTTP(wSessionCookie, reqSessionCookie)
		assert.Equal(t, http.StatusUnauthorized, wSessionCookie.Code)
		assert.Contains(t, wSessionCookie.Header().Get("WWW-Authenticate"), `resource_metadata="`)
		assert.Empty(t, wSessionCookie.Result().Cookies())

		// 5. Invalid/expired browser session cookie on /mcp returns 401 without clearing the cookie
		reqBadCookie := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}`))
		reqBadCookie.AddCookie(&http.Cookie{Name: sessionTokenCookie, Value: "corrupted-session-cookie"})
		wBadCookie := httptest.NewRecorder()
		cookieHandler.ServeHTTP(wBadCookie, reqBadCookie)
		assert.Equal(t, http.StatusUnauthorized, wBadCookie.Code)
		assert.Empty(t, wBadCookie.Result().Cookies())
	})

	t.Run("TransportValidation", func(t *testing.T) {
		_, handler, rawToken, _ := setupMCPTestServer(t)

		// Browser CORS preflight OPTIONS /mcp succeeds before authMiddleware with 204 No Content
		reqOptions := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
		reqOptions.Header.Set("Origin", "https://claude.ai")
		wOptions := httptest.NewRecorder()
		handler.ServeHTTP(wOptions, reqOptions)
		assert.Equal(t, http.StatusNoContent, wOptions.Code)
		assert.Equal(t, "https://claude.ai", wOptions.Header().Get("Access-Control-Allow-Origin"))
		assert.Equal(t, "POST, OPTIONS", wOptions.Header().Get("Access-Control-Allow-Methods"))
		assert.Contains(t, wOptions.Header().Get("Access-Control-Expose-Headers"), "MCP-Protocol-Version")
		assert.Contains(t, wOptions.Header().Get("Access-Control-Expose-Headers"), "WWW-Authenticate")

		// Untrusted or null Origin headers are rejected with 403 Forbidden
		for _, badOrigin := range []string{"http://evil.example.com", "null", "file://"} {
			reqBadOrigin := newReq(rawToken, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{"Origin": badOrigin})
			wBadOrigin := httptest.NewRecorder()
			handler.ServeHTTP(wBadOrigin, reqBadOrigin)
			assert.Equal(t, http.StatusForbidden, wBadOrigin.Code, "origin %q should return 403", badOrigin)
		}

		// Non-JSON or missing Content-Type returns 415 Unsupported Media Type
		reqTextPlain := newReq(rawToken, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{"Content-Type": "text/plain"})
		wTextPlain := httptest.NewRecorder()
		handler.ServeHTTP(wTextPlain, reqTextPlain)
		assert.Equal(t, http.StatusUnsupportedMediaType, wTextPlain.Code)

		reqMissingCT := newReq(rawToken, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{"Content-Type": ""})
		wMissingCT := httptest.NewRecorder()
		handler.ServeHTTP(wMissingCT, reqMissingCT)
		assert.Equal(t, http.StatusUnsupportedMediaType, wMissingCT.Code)

		for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut, http.MethodPatch} {
			req := httptest.NewRequest(method, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+rawToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusMethodNotAllowed, w.Code, "method %s should return 405", method)
			assert.Equal(t, "POST", w.Header().Get("Allow"))
			assert.NotContains(t, w.Body.String(), "<!DOCTYPE html>")
			assert.NotContains(t, w.Body.String(), "<div id=\"root\">")
		}

		// Oversized body (>1MB) returns 413 Request Entity Too Large
		oversized := bytes.Repeat([]byte("a"), 1048576+64)
		reqTooLarge := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(oversized))
		reqTooLarge.Header.Set("Authorization", "Bearer "+rawToken)
		reqTooLarge.Header.Set("Content-Type", "application/json")
		reqTooLarge.Header.Set("Accept", "application/json, text/event-stream")
		wTooLarge := httptest.NewRecorder()
		handler.ServeHTTP(wTooLarge, reqTooLarge)
		assert.Equal(t, http.StatusRequestEntityTooLarge, wTooLarge.Code)

		// Missing text/event-stream in Accept returns 400 Bad Request per go-sdk Streamable HTTP check
		reqJsonOnlyAccept := newReq(rawToken, `{"jsonrpc":"2.0","id":7,"method":"ping"}`, map[string]string{
			"Accept":               "application/json",
			"MCP-Protocol-Version": "2025-11-25",
		})
		wJsonOnlyAccept := httptest.NewRecorder()
		handler.ServeHTTP(wJsonOnlyAccept, reqJsonOnlyAccept)
		assert.Equal(t, http.StatusBadRequest, wJsonOnlyAccept.Code)

		// Compliant Streamable HTTP Accept (application/json + text/event-stream, or */*) succeeds with Cache-Control: no-store
		reqValidHeaders := newReq(rawToken, `{"jsonrpc":"2.0","id":8,"method":"ping"}`, map[string]string{
			"Accept":               "application/json, text/event-stream",
			"Content-Type":         "application/json; charset=utf-8",
			"MCP-Protocol-Version": "2025-11-25",
		})
		wValidHeaders := httptest.NewRecorder()
		handler.ServeHTTP(wValidHeaders, reqValidHeaders)
		assert.Equal(t, http.StatusOK, wValidHeaders.Code, wValidHeaders.Body.String())
		assert.Equal(t, "application/json", wValidHeaders.Header().Get("Content-Type"))
		assert.Equal(t, "no-store", wValidHeaders.Header().Get("Cache-Control"))

		// Malformed JSON and non-object bodies are rejected with 400
		for _, badBody := range []string{`{invalid json`, `42`} {
			reqBad := newReq(rawToken, badBody, nil)
			wBad := httptest.NewRecorder()
			handler.ServeHTTP(wBad, reqBad)
			assert.Equal(t, http.StatusBadRequest, wBad.Code, "body %q should return 400", badBody)
		}

		// JSON-RPC batch arrays are rejected for 2025-11-25 and later
		reqBatch := newReq(rawToken, `[{"jsonrpc": "2.0", "id": "b-1", "method": "tools/list"}]`, map[string]string{"MCP-Protocol-Version": "2025-11-25"})
		wBatch := httptest.NewRecorder()
		handler.ServeHTTP(wBatch, reqBatch)
		assert.Equal(t, http.StatusBadRequest, wBatch.Code)
	})

	t.Run("ProtocolVersionAndHeaderValidation", func(t *testing.T) {
		_, handler, rawToken, _ := setupMCPTestServer(t)

		// Unsupported legacy MCP-Protocol-Version header returns 400 Bad Request
		reqBadProto := newReq(rawToken, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{"MCP-Protocol-Version": "1999-01-01"})
		wBadProto := httptest.NewRecorder()
		handler.ServeHTTP(wBadProto, reqBadProto)
		assert.Equal(t, http.StatusBadRequest, wBadProto.Code)

		// Unsupported per-request 2026+ version returns 400 + -32022 UnsupportedProtocolVersionError listing supported versions
		reqFutureProto := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/list",
			"params": { "_meta": { "io.modelcontextprotocol/protocolVersion": "2099-01-01", "io.modelcontextprotocol/clientCapabilities": {} } }
		}`, map[string]string{"MCP-Protocol-Version": "2099-01-01", "Mcp-Method": "tools/list"})
		wFutureProto := httptest.NewRecorder()
		handler.ServeHTTP(wFutureProto, reqFutureProto)
		require.Equal(t, http.StatusBadRequest, wFutureProto.Code, wFutureProto.Body.String())
		futureErr := decodeRPCError(t, wFutureProto)
		assert.Equal(t, -32022, futureErr.Code)
		var futureData struct {
			Supported []string `json:"supported"`
			Requested string   `json:"requested"`
		}
		require.NoError(t, json.Unmarshal(futureErr.Data, &futureData))
		assert.Equal(t, []string{"2026-07-28", "2025-11-25"}, futureData.Supported)
		assert.Equal(t, "2099-01-01", futureData.Requested)

		// params._meta protocolVersion without the MCP-Protocol-Version header returns 400 + -32020
		reqMetaNoHeader := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 3,
			"method": "tools/list",
			"params": { "_meta": `+meta2026+` }
		}`, nil)
		wMetaNoHeader := httptest.NewRecorder()
		handler.ServeHTTP(wMetaNoHeader, reqMetaNoHeader)
		require.Equal(t, http.StatusBadRequest, wMetaNoHeader.Code)
		assert.Equal(t, -32020, decodeRPCError(t, wMetaNoHeader).Code)

		// 2026-07-28 header without params._meta protocolVersion returns 400 + -32602
		reqHeaderNoMeta := newReq(rawToken, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`, headers2026("tools/list", ""))
		wHeaderNoMeta := httptest.NewRecorder()
		handler.ServeHTTP(wHeaderNoMeta, reqHeaderNoMeta)
		require.Equal(t, http.StatusBadRequest, wHeaderNoMeta.Code)
		assert.Equal(t, -32602, decodeRPCError(t, wHeaderNoMeta).Code)

		// MCP-Protocol-Version header vs params._meta version mismatch returns 400 + -32020
		reqMismatchProto := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 5,
			"method": "tools/list",
			"params": { "_meta": { "io.modelcontextprotocol/protocolVersion": "2025-11-25" } }
		}`, headers2026("tools/list", ""))
		wMismatchProto := httptest.NewRecorder()
		handler.ServeHTTP(wMismatchProto, reqMismatchProto)
		require.Equal(t, http.StatusBadRequest, wMismatchProto.Code)
		assert.Equal(t, -32020, decodeRPCError(t, wMismatchProto).Code)

		listBody := `{
			"jsonrpc": "2.0",
			"id": 6,
			"method": "tools/list",
			"params": { "_meta": ` + meta2026 + ` }
		}`
		callBody := `{
			"jsonrpc": "2.0",
			"id": 7,
			"method": "tools/call",
			"params": {
				"_meta": ` + meta2026 + `,
				"name": "get_energy_summary"
			}
		}`

		// SEP-2243: missing Mcp-Method header returns 400 + -32020
		reqNoMethod := newReq(rawToken, listBody, map[string]string{"MCP-Protocol-Version": "2026-07-28"})
		wNoMethod := httptest.NewRecorder()
		handler.ServeHTTP(wNoMethod, reqNoMethod)
		require.Equal(t, http.StatusBadRequest, wNoMethod.Code)
		assert.Equal(t, -32020, decodeRPCError(t, wNoMethod).Code)

		// SEP-2243: Mcp-Method vs body method mismatch returns 400 + -32020
		reqMismatchMethod := newReq(rawToken, listBody, headers2026("tools/call", ""))
		wMismatchMethod := httptest.NewRecorder()
		handler.ServeHTTP(wMismatchMethod, reqMismatchMethod)
		require.Equal(t, http.StatusBadRequest, wMismatchMethod.Code)
		assert.Equal(t, -32020, decodeRPCError(t, wMismatchMethod).Code)

		// SEP-2243: missing Mcp-Name on tools/call returns 400 + -32020
		reqNoName := newReq(rawToken, callBody, headers2026("tools/call", ""))
		wNoName := httptest.NewRecorder()
		handler.ServeHTTP(wNoName, reqNoName)
		require.Equal(t, http.StatusBadRequest, wNoName.Code)
		assert.Equal(t, -32020, decodeRPCError(t, wNoName).Code)

		// SEP-2243: Mcp-Name vs tools/call name mismatch returns 400 + -32020
		reqMismatchName := newReq(rawToken, callBody, headers2026("tools/call", "get_recent_savings"))
		wMismatchName := httptest.NewRecorder()
		handler.ServeHTTP(wMismatchName, reqMismatchName)
		require.Equal(t, http.StatusBadRequest, wMismatchName.Code)
		assert.Equal(t, -32020, decodeRPCError(t, wMismatchName).Code)

		// Notifications under 2026-07-28 still require Mcp-Method
		reqNotif2026 := newReq(rawToken, `{"jsonrpc": "2.0", "method": "notifications/initialized"}`, map[string]string{"MCP-Protocol-Version": "2026-07-28"})
		wNotif2026 := httptest.NewRecorder()
		handler.ServeHTTP(wNotif2026, reqNotif2026)
		require.Equal(t, http.StatusBadRequest, wNotif2026.Code)
		assert.Equal(t, -32020, decodeRPCError(t, wNotif2026).Code)
	})

	t.Run("Discover20260728AndLegacy20251125Shim", func(t *testing.T) {
		_, handler, rawToken, _ := setupMCPTestServer(t)

		// 1. Primary 2026-07-28: server/discover returns capabilities, supportedVersions, serverInfo in _meta, instructions, ttlMs, cacheScope
		reqDiscover := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": "disc-1",
			"method": "server/discover",
			"params": { "_meta": `+meta2026+` }
		}`, headers2026("server/discover", ""))
		wDiscover := httptest.NewRecorder()
		handler.ServeHTTP(wDiscover, reqDiscover)
		require.Equal(t, http.StatusOK, wDiscover.Code, wDiscover.Body.String())
		assert.Equal(t, "no-store", wDiscover.Header().Get("Cache-Control"))
		assert.Equal(t, "noindex, nofollow, noarchive, nosnippet", wDiscover.Header().Get("X-Robots-Tag"))
		assert.Equal(t, "no-referrer", wDiscover.Header().Get("Referrer-Policy"))

		var discResp struct {
			JSONRPC string `json:"jsonrpc"`
			ID      string `json:"id"`
			Result  struct {
				ResultType        string                        `json:"resultType"`
				SupportedVersions []string                      `json:"supportedVersions"`
				Capabilities      map[string]map[string]any     `json:"capabilities"`
				Meta              map[string]mcp.Implementation `json:"_meta"`
				Instructions      string                        `json:"instructions"`
				TTLMs             int                           `json:"ttlMs"`
				CacheScope        string                        `json:"cacheScope"`
			} `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wDiscover.Body).Decode(&discResp))
		assert.Equal(t, "2.0", discResp.JSONRPC)
		assert.Equal(t, "disc-1", discResp.ID)
		assert.Equal(t, "complete", discResp.Result.ResultType)
		assert.Equal(t, []string{"2026-07-28", "2025-11-25"}, discResp.Result.SupportedVersions)
		if assert.Contains(t, discResp.Result.Capabilities, "tools") {
			assert.NotContains(t, discResp.Result.Capabilities["tools"], "listChanged")
		}
		assert.NotContains(t, discResp.Result.Capabilities, "logging")
		serverInfo := discResp.Result.Meta["io.modelcontextprotocol/serverInfo"]
		assert.Equal(t, "raterudder", serverInfo.Name)
		assert.Equal(t, "RateRudder", serverInfo.Title)
		assert.Equal(t, common.Version(), serverInfo.Version)
		assert.Equal(t, "https://raterudder.com", serverInfo.WebsiteURL)
		assert.Contains(t, discResp.Result.Instructions, "get_energy_summary")
		assert.Contains(t, discResp.Result.Instructions, "get_recent_savings")
		assert.Contains(t, discResp.Result.Instructions, "get_daily_history")
		assert.Equal(t, 3600000, discResp.Result.TTLMs)
		assert.Equal(t, "public", discResp.Result.CacheScope)

		// 2. Removed legacy methods (initialize, ping) are rejected with 404 + -32601 under 2026-07-28
		for _, legacyMethod := range []string{"initialize", "ping"} {
			reqRemoved := newReq(rawToken, `{
				"jsonrpc": "2.0",
				"id": "rem-1",
				"method": "`+legacyMethod+`",
				"params": { "_meta": `+meta2026+` }
			}`, headers2026(legacyMethod, ""))
			wRemoved := httptest.NewRecorder()
			handler.ServeHTTP(wRemoved, reqRemoved)
			require.Equal(t, http.StatusNotFound, wRemoved.Code, "%s: %s", legacyMethod, wRemoved.Body.String())
			assert.Equal(t, -32601, decodeRPCError(t, wRemoved).Code)
		}

		// 3. 2025-11-25 shim: initialize negotiates 2025-11-25, notifications/initialized returns 202, ping returns {}
		for _, clientVer := range []string{"2025-11-25", "2025-06-18"} {
			reqInit := newReq(rawToken, `{
				"jsonrpc": "2.0",
				"id": "init-1",
				"method": "initialize",
				"params": {
					"protocolVersion": "`+clientVer+`",
					"capabilities": {},
					"clientInfo": {"name": "legacy-client", "version": "1.0"}
				}
			}`, nil)
			wInit := httptest.NewRecorder()
			handler.ServeHTTP(wInit, reqInit)
			require.Equal(t, http.StatusOK, wInit.Code, wInit.Body.String())

			var initResp struct {
				ID     string `json:"id"`
				Result struct {
					ProtocolVersion string             `json:"protocolVersion"`
					Capabilities    map[string]any     `json:"capabilities"`
					ServerInfo      mcp.Implementation `json:"serverInfo"`
					Instructions    string             `json:"instructions"`
				} `json:"result"`
			}
			require.NoError(t, json.NewDecoder(wInit.Body).Decode(&initResp))
			assert.Equal(t, "init-1", initResp.ID)
			assert.Equal(t, "2025-11-25", initResp.Result.ProtocolVersion, "client version %s", clientVer)
			assert.Contains(t, initResp.Result.Capabilities, "tools")
			assert.Equal(t, "raterudder", initResp.Result.ServerInfo.Name)
			assert.Equal(t, common.Version(), initResp.Result.ServerInfo.Version)
			assert.Contains(t, initResp.Result.Instructions, "get_energy_summary")
		}

		reqNotif := newReq(rawToken, `{"jsonrpc": "2.0", "method": "notifications/initialized"}`, nil)
		wNotif := httptest.NewRecorder()
		handler.ServeHTTP(wNotif, reqNotif)
		assert.Equal(t, http.StatusAccepted, wNotif.Code)
		assert.Empty(t, wNotif.Body.String())

		reqPing := newReq(rawToken, `{"jsonrpc": "2.0", "id": 100, "method": "ping"}`, map[string]string{"MCP-Protocol-Version": "2025-11-25"})
		wPing := httptest.NewRecorder()
		handler.ServeHTTP(wPing, reqPing)
		require.Equal(t, http.StatusOK, wPing.Code)
		var pingResp struct {
			ID     int            `json:"id"`
			Result map[string]any `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wPing.Body).Decode(&pingResp))
		assert.Equal(t, 100, pingResp.ID)
		assert.Empty(t, pingResp.Result)

		// server/discover rejected on 2025-11-25 shim
		reqDiscoverLegacy := newReq(rawToken, `{"jsonrpc":"2.0","id":101,"method":"server/discover"}`, map[string]string{"MCP-Protocol-Version": "2025-11-25"})
		wDiscoverLegacy := httptest.NewRecorder()
		handler.ServeHTTP(wDiscoverLegacy, reqDiscoverLegacy)
		require.Equal(t, http.StatusOK, wDiscoverLegacy.Code)
		assert.Equal(t, -32601, decodeRPCError(t, wDiscoverLegacy).Code)
	})

	t.Run("ToolsList", func(t *testing.T) {
		_, handler, rawToken, _ := setupMCPTestServer(t)

		// 1. Primary 2026-07-28 tools/list includes resultType, ttlMs, and cacheScope
		req := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/list",
			"params": { "_meta": `+meta2026+` }
		}`, headers2026("tools/list", ""))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp struct {
			Result struct {
				ResultType string     `json:"resultType"`
				TTLMs      int        `json:"ttlMs"`
				CacheScope string     `json:"cacheScope"`
				Tools      []mcp.Tool `json:"tools"`
			} `json:"result"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "complete", resp.Result.ResultType)
		assert.Equal(t, 3600000, resp.Result.TTLMs)
		assert.Equal(t, "public", resp.Result.CacheScope)
		require.Len(t, resp.Result.Tools, 3)

		toolMap := make(map[string]mcp.Tool)
		for _, tool := range resp.Result.Tools {
			toolMap[tool.Name] = tool
			assert.NotEmpty(t, tool.Title)
			assert.NotEmpty(t, tool.Description)
			if assert.NotNil(t, tool.Annotations, tool.Name) {
				assert.Equal(t, tool.Title, tool.Annotations.Title)
				assert.True(t, tool.Annotations.ReadOnlyHint)
				if assert.NotNil(t, tool.Annotations.DestructiveHint) {
					assert.False(t, *tool.Annotations.DestructiveHint)
				}
				assert.True(t, tool.Annotations.IdempotentHint)
				if assert.NotNil(t, tool.Annotations.OpenWorldHint) {
					assert.False(t, *tool.Annotations.OpenWorldHint)
				}
			}
			inputSchema, ok := tool.InputSchema.(map[string]any)
			if assert.True(t, ok, tool.Name) {
				assert.Equal(t, "object", inputSchema["type"])
				assert.Equal(t, false, inputSchema["additionalProperties"])
			}
			outputSchema, ok := tool.OutputSchema.(map[string]any)
			if assert.True(t, ok, tool.Name) {
				assert.Equal(t, "object", outputSchema["type"])
			}
		}

		assert.Contains(t, toolMap, "get_energy_summary")
		assert.Contains(t, toolMap, "get_daily_history")
		if assert.Contains(t, toolMap, "get_recent_savings") {
			props, _ := toolMap["get_recent_savings"].InputSchema.(map[string]any)["properties"].(map[string]any)
			days, _ := props["days"].(map[string]any)
			assert.Equal(t, 1.0, days["minimum"])
			assert.Equal(t, 30.0, days["maximum"])
			assert.Equal(t, 7.0, days["default"])
			assert.NotEmpty(t, days["description"])
		}

		// 2. 2025-11-25 shim tools/list omits the 2026-07-28 resultType
		reqLegacy := newReq(rawToken, `{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}`, map[string]string{"MCP-Protocol-Version": "2025-11-25"})
		wLegacy := httptest.NewRecorder()
		handler.ServeHTTP(wLegacy, reqLegacy)
		require.Equal(t, http.StatusOK, wLegacy.Code)

		var rawLegacy struct {
			Result map[string]any `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wLegacy.Body).Decode(&rawLegacy))
		assert.NotContains(t, rawLegacy.Result, "resultType")
		assert.Contains(t, rawLegacy.Result, "tools")
	})

	t.Run("ToolsCall_EnergySummary", func(t *testing.T) {
		_, handler, rawToken, mockStore := setupMCPTestServer(t)

		// 1. Domain error: No ESS configured (2026-07-28 includes resultType: "complete")
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			ESS: "",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqNoESS := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 1,
			"method": "tools/call",
			"params": {
				"_meta": `+meta2026+`,
				"name": "get_energy_summary"
			}
		}`, headers2026("tools/call", "get_energy_summary"))
		wNoESS := httptest.NewRecorder()
		handler.ServeHTTP(wNoESS, reqNoESS)
		require.Equal(t, http.StatusOK, wNoESS.Code, wNoESS.Body.String())

		var respNoESS struct {
			Result toolResult `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wNoESS.Body).Decode(&respNoESS))
		assert.Equal(t, "complete", respNoESS.Result.ResultType)
		assert.True(t, respNoESS.Result.IsError)
		require.NotEmpty(t, respNoESS.Result.Content)
		assert.Contains(t, respNoESS.Result.Content[0].Text, "no ESS configured")

		// 2. Domain error: No latest action available
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			ESS: "tesla",
			Location: &types.SiteLocation{
				TimeZone: "America/Chicago",
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()
		mockStore.On("GetLatestAction", mock.Anything, "site-1").Return((*types.Action)(nil), nil).Once()

		reqNoStatus := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"name": "get_energy_summary",
				"arguments": {}
			}
		}`, nil)
		wNoStatus := httptest.NewRecorder()
		handler.ServeHTTP(wNoStatus, reqNoStatus)
		require.Equal(t, http.StatusOK, wNoStatus.Code)

		var respNoStatus struct {
			Result toolResult `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wNoStatus.Body).Decode(&respNoStatus))
		assert.True(t, respNoStatus.Result.IsError)
		require.NotEmpty(t, respNoStatus.Result.Content)
		assert.Contains(t, respNoStatus.Result.Content[0].Text, "no status available")

		// 3. Unexpected arguments fail input schema validation and are reported as a tool error
		reqExtraArg := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 4,
			"method": "tools/call",
			"params": {
				"name": "get_energy_summary",
				"arguments": {"foo": 1}
			}
		}`, nil)
		wExtraArg := httptest.NewRecorder()
		handler.ServeHTTP(wExtraArg, reqExtraArg)
		require.Equal(t, http.StatusOK, wExtraArg.Code)
		var respExtraArg struct {
			Result toolResult `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wExtraArg.Body).Decode(&respExtraArg))
		assert.True(t, respExtraArg.Result.IsError)

		// 4. Success case (with explicit power directions + usable kWh)
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			ESS: "tesla",
			Location: &types.SiteLocation{
				TimeZone: "America/Chicago",
			},
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		actionTS := time.Date(2026, 6, 15, 14, 20, 0, 0, loc)
		p1Start := time.Date(2026, 6, 15, 14, 0, 0, 0, loc)
		p1End := time.Date(2026, 6, 15, 15, 0, 0, 0, loc)

		mockStore.On("GetLatestAction", mock.Anything, "site-1").Return(&types.Action{
			Timestamp:       actionTS,
			SystemTimestamp: actionTS,
			BatteryMode:     types.BatteryModeLoad,
			SolarMode:       types.SolarModeAny,
			Reason:          types.ActionReasonSufficientBattery,
			Description:     "Powering home from battery",
			ReserveSOC:      20,
			SystemStatus: types.SystemStatus{
				Timestamp:          actionTS,
				BatterySOC:         82.5,
				BatteryCapacityKWH: 13.5,
				BatteryKW:          2.1,
				SolarKW:            5.2,
				HomeKW:             2.4,
				GridKW:             -4.9,
				TimeLocation:       "America/Chicago",
			},
			Plan: &types.Plan{
				Periods: []types.PlanPeriod{
					{
						TSStart:       p1Start,
						TSEnd:         p1End,
						DurationHours: 1.0,
						BatteryMode:   types.BatteryModeLoad,
						SolarMode:     types.SolarModeAny,
						Reason:        types.ActionReasonSufficientBattery,
						StartSOC:      85.0,
						EndSOC:        80.0,
						SolarKWH:      5.0,
						LoadKWH:       2.5,
						GridExportKWH: 2.5,
						ImportDollars: 0.145,
						ExportDollars: 0.082,
					},
				},
			},
		}, nil).Once()

		todayStart := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)
		mockStore.On("GetEnergyHistory", mock.Anything, "site-1", todayStart, fixedNow).Return([]types.DailyEnergyStats{
			{
				TSDayStart:   todayStart,
				TimeLocation: "America/Chicago",
				Hourly: []types.EnergyStats{
					{
						TSHourStart: time.Date(2026, 6, 15, 10, 0, 0, 0, loc),
						SolarKWH:    10.5,
					},
				},
			},
		}, nil).Once()

		reqSuccess := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 3,
			"method": "tools/call",
			"params": {
				"name": "get_energy_summary",
				"arguments": {}
			}
		}`, nil)
		wSuccess := httptest.NewRecorder()
		handler.ServeHTTP(wSuccess, reqSuccess)
		require.Equal(t, http.StatusOK, wSuccess.Code)

		var respSuccess struct {
			Result struct {
				Content           []toolContent   `json:"content"`
				StructuredContent AgentSummaryRes `json:"structuredContent"`
				IsError           bool            `json:"isError"`
			} `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wSuccess.Body).Decode(&respSuccess))
		assert.False(t, respSuccess.Result.IsError, respSuccess.Result.Content)
		require.Len(t, respSuccess.Result.Content, 1)
		assert.Equal(t, "text", respSuccess.Result.Content[0].Type)

		var contentSummary AgentSummaryRes
		require.NoError(t, json.Unmarshal([]byte(respSuccess.Result.Content[0].Text), &contentSummary))
		assert.Equal(t, respSuccess.Result.StructuredContent, contentSummary)

		assert.Equal(t, 82.5, respSuccess.Result.StructuredContent.Status.BatterySOC)
		assert.Equal(t, 13.5, respSuccess.Result.StructuredContent.Status.BatteryCapacityKWH)
		assert.Equal(t, 0.0, respSuccess.Result.StructuredContent.Status.BatteryChargingKW)
		assert.Equal(t, 2.1, respSuccess.Result.StructuredContent.Status.BatteryDischargingKW)
		assert.Equal(t, 0.0, respSuccess.Result.StructuredContent.Status.GridImportKW)
		assert.Equal(t, 4.9, respSuccess.Result.StructuredContent.Status.GridExportKW)
		assert.Equal(t, 10.5, respSuccess.Result.StructuredContent.SolarForecast.TodayActualSolarKWH)
		assert.Equal(t, 0.0, respSuccess.Result.StructuredContent.SolarForecast.TodayRemainingForecastSolarKWH)
		assert.Equal(t, 10.5, respSuccess.Result.StructuredContent.SolarForecast.TodayTotalEstimatedSolarKWH)
	})

	t.Run("ToolsCall_RecentSavings", func(t *testing.T) {
		_, handler, rawToken, mockStore := setupMCPTestServer(t)

		// 1. Unknown arguments and invalid days (< 1, > 30, fractional, or string) fail input schema
		// validation and are reported as isError tool results so the model can self-correct.
		for _, badArgs := range []string{`{"invalidArg": 5}`, `{"days": 0}`, `{"days": -5}`, `{"days": 31}`, `{"days": 7.5}`, `{"days": "seven"}`} {
			reqBadArgs := newReq(rawToken, `{
				"jsonrpc": "2.0",
				"id": 2,
				"method": "tools/call",
				"params": {
					"name": "get_recent_savings",
					"arguments": `+badArgs+`
				}
			}`, nil)
			wBadArgs := httptest.NewRecorder()
			handler.ServeHTTP(wBadArgs, reqBadArgs)
			require.Equal(t, http.StatusOK, wBadArgs.Code)
			var respBadArgs struct {
				Result toolResult `json:"result"`
			}
			require.NoError(t, json.NewDecoder(wBadArgs.Body).Decode(&respBadArgs))
			assert.True(t, respBadArgs.Result.IsError, "arguments %s should be rejected", badArgs)
		}

		// 2. Success with default 7 days including daily breakdown
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone: "America/Chicago",
			},
			MinBatterySOC: 10,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Maybe()

		mockStore.On("GetPriceHistory", mock.Anything, "site-1", mock.Anything, mock.Anything).Return([]types.Price{
			{
				TSStart:       time.Date(2026, 6, 14, 10, 0, 0, 0, loc),
				TSEnd:         time.Date(2026, 6, 14, 11, 0, 0, 0, loc),
				DollarsPerKWH: 0.12,
			},
			{
				TSStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, loc),
				TSEnd:         time.Date(2026, 6, 15, 11, 0, 0, 0, loc),
				DollarsPerKWH: 0.15,
			},
			{
				TSStart:       time.Date(2026, 6, 16, 10, 0, 0, 0, loc),
				TSEnd:         time.Date(2026, 6, 16, 11, 0, 0, 0, loc),
				DollarsPerKWH: 0.15,
			},
		}, nil).Maybe()

		mockStore.On("GetEnergyHistory", mock.Anything, "site-1", mock.Anything, mock.Anything).Return([]types.DailyEnergyStats{
			{
				TSDayStart:   time.Date(2026, 6, 14, 0, 0, 0, 0, loc),
				TimeLocation: "America/Chicago",
				Hourly: []types.EnergyStats{
					{
						TSHourStart:       time.Date(2026, 6, 14, 10, 0, 0, 0, loc),
						SolarKWH:          10.0,
						HomeKWH:           8.0,
						GridImportKWH:     1.0,
						GridExportKWH:     3.0,
						BatteryUsedKWH:    2.0,
						BatteryChargedKWH: 2.0,
					},
				},
			},
			{
				TSDayStart:   time.Date(2026, 6, 15, 0, 0, 0, 0, loc),
				TimeLocation: "America/Chicago",
				Hourly: []types.EnergyStats{
					{
						TSHourStart:       time.Date(2026, 6, 15, 10, 0, 0, 0, loc),
						SolarKWH:          25.0,
						HomeKWH:           18.0,
						GridImportKWH:     5.0,
						GridExportKWH:     12.0,
						BatteryUsedKWH:    8.0,
						BatteryChargedKWH: 8.0,
					},
				},
			},
		}, nil).Maybe()

		mockStore.On("GetActionHistory", mock.Anything, "site-1", mock.Anything, mock.Anything).Return([]types.Action{
			{
				Timestamp: time.Date(2026, 6, 15, 10, 0, 0, 0, loc),
				SystemStatus: types.SystemStatus{
					BatteryCapacityKWH: 13.5,
					BatterySOC:         80.0,
					TimeLocation:       "America/Chicago",
				},
			},
		}, nil).Maybe()

		reqSavings := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 10,
			"method": "tools/call",
			"params": {
				"name": "get_recent_savings"
			}
		}`, nil)
		wSavings := httptest.NewRecorder()
		handler.ServeHTTP(wSavings, reqSavings)
		require.Equal(t, http.StatusOK, wSavings.Code)

		// Ensure HourlyDebugging is never exposed to MCP clients
		bodyStr := wSavings.Body.String()
		assert.NotContains(t, bodyStr, "hourlyDebugging")
		assert.NotContains(t, bodyStr, "HourlyDebugging")

		var respSavings struct {
			Result struct {
				Content           []toolContent       `json:"content"`
				StructuredContent MCPRecentSavingsRes `json:"structuredContent"`
				IsError           bool                `json:"isError"`
			} `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wSavings.Body).Decode(&respSavings))
		assert.False(t, respSavings.Result.IsError, respSavings.Result.Content)
		require.Len(t, respSavings.Result.Content, 1)
		var contentSavings MCPRecentSavingsRes
		require.NoError(t, json.Unmarshal([]byte(respSavings.Result.Content[0].Text), &contentSavings))
		assert.Equal(t, respSavings.Result.StructuredContent, contentSavings)

		assert.Equal(t, 7, respSavings.Result.StructuredContent.Days)
		assert.Equal(t, "America/Chicago", respSavings.Result.StructuredContent.TimeZone)
		assert.Equal(t, 35.0, respSavings.Result.StructuredContent.SolarGeneratedKWH)
		assert.Equal(t, 26.0, respSavings.Result.StructuredContent.HomeUsedKWH)
		assert.Equal(t, 10.0, respSavings.Result.StructuredContent.BatteryDischargedKWH)
		if assert.Len(t, respSavings.Result.StructuredContent.Daily, 7) {
			assert.Equal(t, "2026-06-14", respSavings.Result.StructuredContent.Daily[5].Date)
			assert.Equal(t, 10.0, respSavings.Result.StructuredContent.Daily[5].SolarGeneratedKWH)
			assert.Equal(t, 2.0, respSavings.Result.StructuredContent.Daily[5].BatteryDischargedKWH)
			assert.Equal(t, "2026-06-15", respSavings.Result.StructuredContent.Daily[6].Date)
			assert.Equal(t, 25.0, respSavings.Result.StructuredContent.Daily[6].SolarGeneratedKWH)
			assert.Equal(t, 8.0, respSavings.Result.StructuredContent.Daily[6].BatteryDischargedKWH)
		}
	})

	t.Run("ToolsCall_DailyHistory", func(t *testing.T) {
		_, handler, rawToken, mockStore := setupMCPTestServer(t)

		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			Location: &types.SiteLocation{
				TimeZone: "America/Chicago",
			},
			GridExportSolar: true,
			MinBatterySOC:   15.0,
		}, types.CurrentSettingsVersion, time.Time{}, nil).Maybe()

		// 1. Invalid or future date rejected with JSON-RPC -32602 Invalid params
		for _, badArgs := range []string{`{"date": "not-a-date"}`, `{"date": "2026-06-16"}`} {
			reqBad := newReq(rawToken, `{
				"jsonrpc": "2.0",
				"id": 1,
				"method": "tools/call",
				"params": {
					"name": "get_daily_history",
					"arguments": `+badArgs+`
				}
			}`, nil)
			wBad := httptest.NewRecorder()
			handler.ServeHTTP(wBad, reqBad)
			require.Equal(t, http.StatusOK, wBad.Code)
			assert.Equal(t, -32602, decodeRPCError(t, wBad).Code, "arguments %s", badArgs)
		}

		// Under 2026-07-28, -32602 maps to HTTP 400
		reqBad2026 := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 2,
			"method": "tools/call",
			"params": {
				"_meta": `+meta2026+`,
				"name": "get_daily_history",
				"arguments": {"date": "2026-06-16"}
			}
		}`, headers2026("tools/call", "get_daily_history"))
		wBad2026 := httptest.NewRecorder()
		handler.ServeHTTP(wBad2026, reqBad2026)
		require.Equal(t, http.StatusBadRequest, wBad2026.Code)
		assert.Equal(t, -32602, decodeRPCError(t, wBad2026).Code)

		// Unknown arguments fail input schema validation (isError tool result)
		reqUnknown := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 3,
			"method": "tools/call",
			"params": {
				"name": "get_daily_history",
				"arguments": {"unknown": "x"}
			}
		}`, nil)
		wUnknown := httptest.NewRecorder()
		handler.ServeHTTP(wUnknown, reqUnknown)
		require.Equal(t, http.StatusOK, wUnknown.Code)
		var respUnknown struct {
			Result toolResult `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wUnknown.Body).Decode(&respUnknown))
		assert.True(t, respUnknown.Result.IsError)

		// 2. Valid past day (2026-06-14) returns hourly telemetry, rates (including $0.00/kWh vs missing), and collapsed decision timeline
		dayStart := time.Date(2026, 6, 14, 0, 0, 0, 0, loc)
		dayEnd := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)

		mockStore.On("GetEnergyHistory", mock.Anything, "site-1", dayStart, dayEnd).Return([]types.DailyEnergyStats{
			{
				TSDayStart:   dayStart,
				TimeLocation: "America/Chicago",
				Hourly: []types.EnergyStats{
					{
						TSHourStart:       time.Date(2026, 6, 14, 2, 0, 0, 0, loc),
						SolarKWH:          0,
						HomeKWH:           1.2,
						BatteryChargedKWH: 4.5,
						GridImportKWH:     5.7,
						MinBatterySOC:     25.0,
						MaxBatterySOC:     58.0,
					},
					{
						TSHourStart:   time.Date(2026, 6, 14, 10, 0, 0, 0, loc),
						SolarKWH:      3.0,
						HomeKWH:       1.0,
						GridExportKWH: 2.0,
						MinBatterySOC: 60.0,
						MaxBatterySOC: 60.0,
					},
					{
						TSHourStart:        time.Date(2026, 6, 14, 15, 0, 0, 0, loc),
						SolarKWH:           6.4,
						HomeKWH:            2.0,
						BatteryUsedKWH:     1.5,
						GridExportKWH:      5.9,
						MinBatterySOC:      70.0,
						MaxBatterySOC:      82.0,
						HadGridUnavailable: true,
						Alarms:             []types.SystemAlarm{{Name: "Grid Overvoltage"}},
					},
					{
						TSHourStart:   time.Date(2026, 6, 14, 20, 0, 0, 0, loc),
						SolarKWH:      0,
						HomeKWH:       0.8,
						GridImportKWH: 0.8,
						MinBatterySOC: 70.0,
						MaxBatterySOC: 70.0,
					},
				},
			},
		}, nil).Once()

		// Cross-midnight TOU interval starting 22:00 previous night is captured via 24h lookback/lookahead window
		mockStore.On("GetPriceHistory", mock.Anything, "site-1", dayStart.Add(-24*time.Hour), dayEnd.Add(24*time.Hour)).Return([]types.Price{
			{
				TSStart:       time.Date(2026, 6, 13, 22, 0, 0, 0, loc),
				TSEnd:         time.Date(2026, 6, 14, 6, 0, 0, 0, loc),
				DollarsPerKWH: 0.025,
			},
			{
				TSStart:       time.Date(2026, 6, 14, 10, 0, 0, 0, loc),
				TSEnd:         time.Date(2026, 6, 14, 11, 0, 0, 0, loc),
				DollarsPerKWH: 0.0,
			},
			{
				TSStart:       time.Date(2026, 6, 14, 15, 0, 0, 0, loc),
				TSEnd:         time.Date(2026, 6, 14, 16, 0, 0, 0, loc),
				DollarsPerKWH: 0.22,
			},
		}, nil).Once()

		// Two consecutive 20-min charge actions followed by a peak discharge action, then a paused action (fallback ReserveSOC)
		mockStore.On("GetActionHistory", mock.Anything, "site-1", dayStart, dayEnd).Return([]types.Action{
			{
				Timestamp:   time.Date(2026, 6, 14, 2, 0, 0, 0, loc),
				BatteryMode: types.BatteryModeChargeAny,
				SolarMode:   types.SolarModeAny,
				Reason:      types.ActionReasonDeficitChargeNow,
				Description: "Pre-charging at $0.025/kWh ahead of evening peak",
				ReserveSOC:  20,
				SystemStatus: types.SystemStatus{
					BatterySOC: 25.0,
				},
			},
			{
				Timestamp:   time.Date(2026, 6, 14, 2, 20, 0, 0, loc),
				BatteryMode: types.BatteryModeChargeAny,
				SolarMode:   types.SolarModeAny,
				Reason:      types.ActionReasonDeficitChargeNow,
				Description: "Pre-charging at $0.025/kWh ahead of evening peak",
				ReserveSOC:  20,
				SystemStatus: types.SystemStatus{
					BatterySOC: 48.0,
				},
			},
			{
				Timestamp:   time.Date(2026, 6, 14, 15, 0, 0, 0, loc),
				BatteryMode: types.BatteryModeExport,
				SolarMode:   types.SolarModeExport,
				Reason:      types.ActionReasonDischargeAtPeak,
				Description: "Exporting battery during $0.22/kWh peak",
				ReserveSOC:  20,
				SystemStatus: types.SystemStatus{
					BatterySOC: 82.0,
				},
			},
			{
				Timestamp:   time.Date(2026, 6, 14, 15, 20, 0, 0, loc),
				BatteryMode: types.BatteryModeNoChange,
				SolarMode:   types.SolarModeNoChange,
				Paused:      true,
				ReserveSOC:  0, // paused action omits ReserveSOC; falls back to settings.MinBatterySOC (10%)
				SystemStatus: types.SystemStatus{
					BatterySOC: 75.0,
				},
			},
		}, nil).Once()

		reqHist := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": "hist-1",
			"method": "tools/call",
			"params": {
				"name": "get_daily_history",
				"arguments": {"date": "2026-06-14"}
			}
		}`, nil)
		wHist := httptest.NewRecorder()
		handler.ServeHTTP(wHist, reqHist)
		require.Equal(t, http.StatusOK, wHist.Code)

		var respHist struct {
			Result struct {
				Content           []toolContent      `json:"content"`
				StructuredContent MCPDailyHistoryRes `json:"structuredContent"`
				IsError           bool               `json:"isError"`
			} `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wHist.Body).Decode(&respHist))
		assert.False(t, respHist.Result.IsError, respHist.Result.Content)
		require.Len(t, respHist.Result.Content, 1)
		var contentHist MCPDailyHistoryRes
		require.NoError(t, json.Unmarshal([]byte(respHist.Result.Content[0].Text), &contentHist))
		assert.Equal(t, respHist.Result.StructuredContent, contentHist)

		assert.Equal(t, "2026-06-14", respHist.Result.StructuredContent.Date)
		assert.Equal(t, 9.4, respHist.Result.StructuredContent.TotalSolarKWH)
		assert.Equal(t, 5.0, respHist.Result.StructuredContent.TotalHomeUsedKWH)
		assert.Equal(t, 1.5, respHist.Result.StructuredContent.TotalBatteryDischargedKWH)
		assert.True(t, respHist.Result.StructuredContent.HadGridUnavailable)
		assert.Equal(t, []string{"Grid Overvoltage"}, respHist.Result.StructuredContent.AlarmNames)
		if assert.Len(t, respHist.Result.StructuredContent.Decisions, 3) {
			assert.Equal(t, 2, respHist.Result.StructuredContent.Decisions[0].PollCount)
			// Multi-hour telemetry gap (02:20 -> 15:00) must NOT stretch Decisions[0].TSEnd past 02:40
			assert.True(t, respHist.Result.StructuredContent.Decisions[0].TSEnd.Equal(time.Date(2026, 6, 14, 2, 40, 0, 0, loc)))
			assert.Equal(t, 25.0, respHist.Result.StructuredContent.Decisions[0].StartBatterySOC)
			assert.Equal(t, 48.0, respHist.Result.StructuredContent.Decisions[0].EndBatterySOC)
			assert.Equal(t, "Charge from Grid/Solar", respHist.Result.StructuredContent.Decisions[0].BatteryModeLabel)
			// Contiguous transition (15:00 -> 15:20) snaps Decisions[1].TSEnd to 15:20
			assert.True(t, respHist.Result.StructuredContent.Decisions[1].TSEnd.Equal(time.Date(2026, 6, 14, 15, 20, 0, 0, loc)))
			assert.Equal(t, "Export to Grid", respHist.Result.StructuredContent.Decisions[1].BatteryModeLabel)
			// Paused segment uses configured MinBatterySOC fallback (15.0%)
			assert.True(t, respHist.Result.StructuredContent.Decisions[2].Paused)
			assert.Equal(t, 15.0, respHist.Result.StructuredContent.Decisions[2].ReserveSOC)
		}
		if assert.Len(t, respHist.Result.StructuredContent.Hours, 4) {
			if assert.NotNil(t, respHist.Result.StructuredContent.Hours[0].ImportDollarsPerKWH) {
				assert.Equal(t, 0.025, *respHist.Result.StructuredContent.Hours[0].ImportDollarsPerKWH)
			}
			// $0.00/kWh price is preserved as non-nil 0.0 (not swallowed by omitempty)
			if assert.NotNil(t, respHist.Result.StructuredContent.Hours[1].ImportDollarsPerKWH) {
				assert.Equal(t, 0.0, *respHist.Result.StructuredContent.Hours[1].ImportDollarsPerKWH)
			}
			if assert.NotNil(t, respHist.Result.StructuredContent.Hours[2].ImportDollarsPerKWH) {
				assert.Equal(t, 0.22, *respHist.Result.StructuredContent.Hours[2].ImportDollarsPerKWH)
			}
			assert.Equal(t, 1.5, respHist.Result.StructuredContent.Hours[2].BatteryDischargedKWH)
			// Missing price is nil (omitted in JSON)
			assert.Nil(t, respHist.Result.StructuredContent.Hours[3].ImportDollarsPerKWH)
			assert.Nil(t, respHist.Result.StructuredContent.Hours[3].ExportDollarsPerKWH)
		}
	})

	t.Run("JSONRPCErrorsAndLegacyToolsCall", func(t *testing.T) {
		_, handler, rawToken, mockStore := setupMCPTestServer(t)

		// 1. Invalid Request: missing jsonrpc version, or null/boolean/object id
		for _, badReq := range []string{
			`{"id": 1, "method": "ping"}`,
			`{"jsonrpc": "2.0", "id": null, "method": "ping"}`,
			`{"jsonrpc": "2.0", "id": true, "method": "ping"}`,
			`{"jsonrpc": "2.0", "id": {}, "method": "ping"}`,
		} {
			reqBad := newReq(rawToken, badReq, map[string]string{"MCP-Protocol-Version": "2025-11-25"})
			wBad := httptest.NewRecorder()
			handler.ServeHTTP(wBad, reqBad)
			assert.Equal(t, http.StatusBadRequest, wBad.Code, "request %s: %s", badReq, wBad.Body.String())
		}

		// 2. Method not found under 2026-07-28 returns 404 + -32601
		reqUnknown := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 99,
			"method": "unknown/method",
			"params": { "_meta": `+meta2026+` }
		}`, headers2026("unknown/method", ""))
		wUnknown := httptest.NewRecorder()
		handler.ServeHTTP(wUnknown, reqUnknown)
		require.Equal(t, http.StatusNotFound, wUnknown.Code)
		assert.Equal(t, -32601, decodeRPCError(t, wUnknown).Code)

		// 3. Unknown tool in tools/call (-32602)
		reqUnknownTool := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 101,
			"method": "tools/call",
			"params": { "name": "nonexistent_tool" }
		}`, nil)
		wUnknownTool := httptest.NewRecorder()
		handler.ServeHTTP(wUnknownTool, reqUnknownTool)
		require.Equal(t, http.StatusOK, wUnknownTool.Code)
		assert.Equal(t, -32602, decodeRPCError(t, wUnknownTool).Code)

		// 4. 2025-11-25 shim tools/call omits 2026-07-28 resultType
		mockStore.On("GetSettings", mock.Anything, "site-1").Return(types.Settings{
			ESS: "",
		}, types.CurrentSettingsVersion, time.Time{}, nil).Once()

		reqLegacyCall := newReq(rawToken, `{
			"jsonrpc": "2.0",
			"id": 202,
			"method": "tools/call",
			"params": { "name": "get_energy_summary" }
		}`, map[string]string{"MCP-Protocol-Version": "2025-11-25"})
		wLegacyCall := httptest.NewRecorder()
		handler.ServeHTTP(wLegacyCall, reqLegacyCall)
		require.Equal(t, http.StatusOK, wLegacyCall.Code)
		var rawLegacyCall struct {
			Result map[string]any `json:"result"`
		}
		require.NoError(t, json.NewDecoder(wLegacyCall.Body).Decode(&rawLegacyCall))
		assert.NotContains(t, rawLegacyCall.Result, "resultType")
		assert.Equal(t, true, rawLegacyCall.Result["isError"])
	})
}
