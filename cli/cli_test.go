package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// Runs the CLI with input as standard input and returns its standard output.
func run(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	stdin, stdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = stdin, stdout }()
	inRead, inWrite, _ := os.Pipe()
	outRead, outWrite, _ := os.Pipe()
	os.Stdin, os.Stdout = inRead, outWrite
	go func() {
		io.WriteString(inWrite, input)
		inWrite.Close()
	}()
	output := make(chan string)
	go func() {
		all, _ := io.ReadAll(outRead)
		output <- string(all)
	}()
	root := rootCommand()
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	outWrite.Close()
	return <-output, err
}

func TestSignInThenGraphQLThenSignOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var challenge string
	var deleted, revoked bool
	access := "access-1"
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("DEPLOYYY_API", server.URL)
	reply := func(w http.ResponseWriter, value any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(value)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/api/graphql", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]any{"authorization_servers": []string{server.URL}, "scopes_supported": []string{"openid", "offline_access"}})
	})
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, map[string]string{
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"registration_endpoint":  server.URL + "/register",
			"revocation_endpoint":    server.URL + "/revoke",
		})
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		reply(w, map[string]string{"client_id": "cli", "registration_client_uri": server.URL + "/register/cli", "registration_access_token": "registration"})
	})
	mux.HandleFunc("DELETE /register/cli", func(w http.ResponseWriter, r *http.Request) {
		deleted = r.Header.Get("Authorization") == "Bearer registration"
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("client_id") != "cli" || query.Get("code_challenge_method") != "S256" || query.Get("scope") != "openid offline_access" || !strings.HasPrefix(query.Get("redirect_uri"), "http://127.0.0.1:") {
			t.Errorf("authorize request: %s", r.URL.RawQuery)
		}
		challenge = query.Get("code_challenge")
		http.Redirect(w, r, query.Get("redirect_uri")+"?code=code-1&state="+query.Get("state"), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("grant_type") == "refresh_token" {
			if r.FormValue("refresh_token") != "refresh-1" || r.FormValue("client_id") != "cli" {
				t.Errorf("refresh request: %v", r.Form)
			}
			access = "access-2"
			reply(w, map[string]any{"access_token": access, "refresh_token": "refresh-2", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		verifier := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if r.FormValue("code") != "code-1" || base64.RawURLEncoding.EncodeToString(verifier[:]) != challenge {
			t.Errorf("token request: %v", r.Form)
		}
		reply(w, map[string]any{"access_token": access, "refresh_token": "refresh-1", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("POST /revoke", func(_ http.ResponseWriter, r *http.Request) {
		revoked = r.FormValue("token") == "refresh-2"
	})
	mux.HandleFunc("POST /api/graphql", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string
			Variables map[string]any
		}
		json.NewDecoder(r.Body).Decode(&request)
		if r.Header.Get("Authorization") != "Bearer "+access || strings.Contains(request.Query, "refused") {
			reply(w, map[string]any{"errors": []map[string]string{{"message": "Unauthenticated"}}})
			return
		}
		reply(w, map[string]any{"data": map[string]any{"viewer": map[string]any{"preferredLoginName": "dev@example.com"}, "variables": request.Variables}})
	})
	openBrowser = func(address string) {
		go func() {
			response, err := http.Get(address)
			if err == nil {
				response.Body.Close()
			}
		}()
	}

	if _, err := run(t, "", "login"); err != nil {
		t.Fatal(err)
	}
	output, err := run(t, "", "graphql", "{ viewer { preferredLoginName } }", "-v", "first=2", "-v", "slug=acme", "-v", `input={"a":true}`)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Data struct{ Variables map[string]any }
	}
	json.Unmarshal([]byte(output), &response)
	if got, _ := json.Marshal(response.Data.Variables); string(got) != `{"first":2,"input":{"a":true},"slug":"acme"}` {
		t.Errorf("variables: %s", got)
	}

	// An expired access token: the next call takes a new one with the refresh token and saves the pair.
	file := filepath.Join(os.Getenv("HOME"), ".config", "deployyy", "accounts.json")
	accounts, _ := os.ReadFile(file)
	saved := map[string]map[string]any{}
	json.Unmarshal(accounts, &saved)
	saved[server.URL]["token"].(map[string]any)["expiry"] = "2020-01-01T00:00:00Z"
	accounts, _ = json.Marshal(saved)
	os.WriteFile(file, accounts, 0o600)
	if _, err := run(t, "{ viewer { preferredLoginName } }", "graphql"); err != nil {
		t.Fatal(err)
	}
	if accounts, _ = os.ReadFile(file); !strings.Contains(string(accounts), "refresh-2") || strings.Contains(string(accounts), "refresh-1") {
		t.Errorf("the saved sign-in after a refresh: %s", accounts)
	}

	if output, err := run(t, "", "graphql", "{ refused }"); err == nil || !strings.Contains(output, "Unauthenticated") {
		t.Errorf("a response with errors: %v %s", err, output)
	}

	if _, err := run(t, "", "logout"); err != nil || !deleted || !revoked {
		t.Errorf("logout: %v, client deleted %v, token revoked %v", err, deleted, revoked)
	}
	if _, err := run(t, "", "graphql", "{ viewer { id } }"); err == nil || !strings.Contains(err.Error(), "deployyy login") {
		t.Errorf("after logout: %v", err)
	}
}

func TestSubscriptionPrintsEachEvent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DEPLOYYY_TOKEN", "token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Errorf("accept: %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ":\n\nevent: next\ndata: {\"data\":{\"tick\":1},\"extensions\":{}}\n\nevent: next\ndata: {\"errors\":[{\"message\":\"late\"}]}\n\nevent: complete\ndata:\n\n")
	}))
	defer server.Close()
	t.Setenv("DEPLOYYY_API", server.URL)

	output, err := run(t, "", "graphql", "subscription { tick }")
	if err == nil || output != "{\"data\":{\"tick\":1}}\n{\"errors\":[{\"message\":\"late\"}]}\n" {
		t.Errorf("subscription: %v, output %q", err, output)
	}
}

func TestShellRunsAScript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DEPLOYYY_TOKEN", "token")
	polls := 0
	script := "echo catalog_product_price\n" + strings.Repeat("# a long script\n", 100) + "printf Ready; sh -c 'exit 3'"
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("DEPLOYYY_API", server.URL)
	mux.HandleFunc("POST /api/graphql", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string
			Variables map[string]any
		}
		json.NewDecoder(r.Body).Decode(&request)
		switch {
		case strings.Contains(request.Query, "teams(filter"):
			if got, _ := json.Marshal(request.Variables); string(got) != `{"app":"magento","branch":"feature/cart","project":"shop","shell":null,"team":"acme"}` {
				t.Errorf("scope: %s", got)
			}
			io.WriteString(w, `{"data":{"viewer":{"teams":{"nodes":[{"projects":{"nodes":[{"apps":{"nodes":[{"environments":{"nodes":[{"id":"environment-1","level":"STAGING","actions":{"shellCreate":{"allowed":true}}}]}}]}}]}}]}}}}`)
		case strings.Contains(request.Query, "environmentShellCreate"):
			io.WriteString(w, `{"data":{"environmentShellCreate":{"changeId":"change-1"}}}`)
		case strings.Contains(request.Query, "node(id"):
			polls++
			fmt.Fprintf(w, `{"data":{"node":{"status":{"phase":"PENDING","message":"Waiting for environment main to wake."},"actions":{"shellAttach":{"allowed":%t}}}}}`, polls > 1)
		case strings.Contains(request.Query, "environmentShellAttach"):
			fmt.Fprintf(w, `{"data":{"environmentShellAttach":{"url":"ws%s/v1/attach","ticket":"ticket-1"}}}`, strings.TrimPrefix(server.URL, "http"))
		}
	})
	left := make(chan websocket.StatusCode, 1)
	mux.HandleFunc("GET /v1/attach", func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil || r.URL.Query().Get("ticket") != "ticket-1" || r.URL.Query().Get("terminal") != "script" {
			t.Errorf("attach: %v %s", err, r.URL.RawQuery)
			return
		}
		socket.Write(r.Context(), websocket.MessageText, []byte(`{"message":"The shell ends in 7 days"}`))
		// The output of an earlier script, then the prompt.
		socket.Write(r.Context(), websocket.MessageBinary, []byte("\x1b]7770;__deployyy_OLD_start\x07old output\x1b]7770;__deployyy_OLD_end_0\x07\r\n/app$ "))
		var typed []byte
		for !bytes.Contains(typed, []byte("stty echo\n")) {
			_, data, err := socket.Read(r.Context())
			if err != nil {
				t.Errorf("typed: %v", err)
				return
			}
			typed = append(typed, data...)
		}
		// A real shell runs the typed lines. The terminal echoes the last line, ends each line with CR LF and sends small frames.
		shell := exec.Command("sh")
		shell.Stdin = bytes.NewReader(typed)
		printed, _ := shell.Output()
		echo := typed[bytes.LastIndex(typed[:len(typed)-1], []byte("\n"))+1:]
		reply := append(echo, bytes.ReplaceAll(printed, []byte("\n"), []byte("\r\n"))...)
		for len(reply) > 0 {
			frame := min(len(reply), 7)
			socket.Write(r.Context(), websocket.MessageBinary, reply[:frame])
			reply = reply[frame:]
		}
		_, _, err = socket.Read(r.Context())
		left <- websocket.CloseStatus(err)
	})

	output, err := run(t, script, "shell", "acme/shop/magento/feature/cart")
	if status := exitStatus(0); !errors.As(err, &status) || status != 3 || output != "catalog_product_price\nReady" || polls != 2 {
		t.Errorf("shell: %v, output %q, polls %d", err, output, polls)
	}
	// The CLI leaves the shell open for the next script.
	if code := <-left; code != websocket.StatusNormalClosure {
		t.Errorf("the CLI closed with %d", code)
	}
}

func TestUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installed := filepath.Join(t.TempDir(), "deployyy")
	os.WriteFile(installed, []byte("old"), 0o755)
	executable = func() (string, error) { return installed, nil }
	name := fmt.Sprintf("deployyy-%s-%s", runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256([]byte("new"))
	sums := hex.EncodeToString(sum[:]) + "  " + name + "\n"
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	releases = server.URL
	mux.HandleFunc("HEAD /latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/v9.9.9", http.StatusFound)
	})
	mux.HandleFunc("GET /download/v9.9.9/"+name, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "new") })
	mux.HandleFunc("GET /download/v9.9.9/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, sums) })

	version = "v9.9.9"
	if _, err := run(t, "", "update"); err != nil {
		t.Fatal(err)
	}
	if program, _ := os.ReadFile(installed); string(program) != "old" {
		t.Errorf("the newest release was replaced by %q", program)
	}

	version = "v1.0.0"
	sums = strings.Repeat("0", 64) + "  " + name + "\n"
	if _, err := run(t, "", "update"); err == nil {
		t.Error("a wrong checksum passed")
	}
	if program, _ := os.ReadFile(installed); string(program) != "old" {
		t.Errorf("a wrong checksum installed %q", program)
	}

	sums = hex.EncodeToString(sum[:]) + "  " + name + "\n"
	if _, err := run(t, "", "update"); err != nil {
		t.Fatal(err)
	}
	if program, _ := os.ReadFile(installed); string(program) != "new" {
		t.Errorf("the update installed %q", program)
	}
	if mode, _ := os.Stat(installed); mode.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", mode.Mode())
	}
}

func TestRefreshFollowsAMovedSignInService(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("DEPLOYYY_API", server.URL)
	mux.HandleFunc("/.well-known/oauth-protected-resource/api/graphql", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{server.URL + "/new"}})
	})
	mux.HandleFunc("/new/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"token_endpoint":        server.URL + "/new/token",
			"revocation_endpoint":   server.URL + "/new/revoke",
			"registration_endpoint": server.URL + "/new/register",
		})
	})
	mux.HandleFunc("/new/token", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("refresh_token") != "refresh" {
			t.Errorf("refresh token: %q", r.FormValue("refresh_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"fresh","token_type":"Bearer","refresh_token":"refresh2","expires_in":3600}`)
	})
	mux.HandleFunc("/api/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			t.Errorf("authorization: %q", r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{"data":{"viewer":{"id":"v"}}}`)
	})
	// The saved endpoints name a host that no longer answers.
	file := filepath.Join(os.Getenv("HOME"), ".config", "deployyy", "accounts.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	saved := map[string]any{server.URL: map[string]any{
		"client_id":               "client",
		"registration_client_uri": "http://127.0.0.1:1/old/register/client",
		"token_endpoint":          "http://127.0.0.1:1/old/token",
		"revocation_endpoint":     "http://127.0.0.1:1/old/revoke",
		"token":                   map[string]any{"access_token": "stale", "refresh_token": "refresh", "expiry": "2000-01-01T00:00:00Z"},
	}}
	body, _ := json.Marshal(saved)
	if err := os.WriteFile(file, body, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, "", "graphql", "{ viewer { id } }"); err != nil {
		t.Fatalf("graphql: %v", err)
	}
	stored, _ := os.ReadFile(file)
	for _, want := range []string{server.URL + "/new/token", server.URL + "/new/revoke", strings.TrimPrefix(server.URL, "http://") + "/old/register/client", "refresh2"} {
		if !strings.Contains(string(stored), want) {
			t.Errorf("accounts.json lacks %s: %s", want, stored)
		}
	}
}
