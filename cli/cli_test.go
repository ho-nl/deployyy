package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

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
			io.WriteString(w, `{"data":{"viewer":{"teams":{"nodes":[{"projects":{"nodes":[{"id":"project-1","apps":{"nodes":[{"environments":{"nodes":[{"id":"environment-1","level":"STAGING","actions":{"shellCreate":{"allowed":true}}}]}}]}}]}}]}}}}`)
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

// fakeFiles is the Files area of one project and its object store: the upload of one file in parts of partSize bytes.
type fakeFiles struct {
	t        *testing.T
	partSize int64
	size     int64
	phase    string
	listed   []int          // the parts that the status lists: the parts at the last grant
	stored   map[int][]byte // the parts in the store
	failOnce map[int]bool
	puts     []int
	request  string
	granted  []any
	input    map[string]any
}

func (f *fakeFiles) parts() int { return int((f.size + f.partSize - 1) / f.partSize) }

func (f *fakeFiles) status() map[string]any {
	listed := []int{}
	listed = append(listed, f.listed...)
	return map[string]any{"phase": f.phase, "partCount": f.parts(), "partSize": f.partSize, "uploadedParts": listed, "requestId": f.request}
}

func (f *fakeFiles) serve(mux *http.ServeMux, server func() string) {
	mux.HandleFunc("PUT /store/{part}", func(w http.ResponseWriter, r *http.Request) {
		var part int
		fmt.Sscan(r.PathValue("part"), &part)
		if r.ContentLength <= 0 || len(r.TransferEncoding) > 0 {
			f.t.Errorf("part %d is sent without a length: %d %v", part, r.ContentLength, r.TransferEncoding)
		}
		body, _ := io.ReadAll(r.Body)
		if f.failOnce[part] {
			delete(f.failOnce, part)
			http.Error(w, "SlowDown", http.StatusServiceUnavailable)
			return
		}
		f.puts = append(f.puts, part)
		f.stored[part] = body
	})
	mux.HandleFunc("POST /api/graphql", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string
			Variables map[string]any
		}
		json.NewDecoder(r.Body).Decode(&request)
		reply := func(data any) { json.NewEncoder(w).Encode(map[string]any{"data": data}) }
		switch {
		case strings.Contains(request.Query, "projects(filter"):
			io.WriteString(w, `{"data":{"viewer":{"teams":{"nodes":[{"projects":{"nodes":[{"id":"project-1"}]}}]}}}}`)
		case strings.Contains(request.Query, "projectFileUploadStart"):
			f.input = request.Variables["input"].(map[string]any)
			reply(map[string]any{"projectFileUploadStart": map[string]any{"file": map[string]any{"id": "file-1", "status": map[string]any{"phase": f.phase}}}})
		case strings.Contains(request.Query, "projectFileUploadUpdate"):
			settings := request.Variables["input"].(map[string]any)["settings"].(map[string]any)
			if settings["complete"] == true {
				f.phase = "Ready"
			} else {
				// The operator lists the stored parts when it signs a grant.
				f.request, f.granted, f.listed = settings["requestId"].(string), settings["parts"].([]any), nil
				for part := 1; part <= f.parts(); part++ {
					if f.stored[part] != nil {
						f.listed = append(f.listed, part)
					}
				}
			}
			reply(map[string]any{"projectFileUploadUpdate": map[string]any{"file": map[string]any{"id": "file-1"}}})
		case strings.Contains(request.Query, "projectFileUploadClaim"):
			parts := []map[string]any{}
			for _, part := range f.granted {
				parts = append(parts, map[string]any{"partNumber": part, "putUrl": fmt.Sprintf("%s/store/%v", server(), part)})
			}
			reply(map[string]any{"projectFileUploadClaim": map[string]any{"upload": map[string]any{"requestId": f.request, "parts": parts}}})
		case strings.Contains(request.Query, "on ProjectFile"):
			reply(map[string]any{"node": map[string]any{"status": f.status()}})
		default:
			f.t.Errorf("unexpected query %s", request.Query)
		}
	})
}

// An upload resumes: the parts that the store holds are not sent again, a part that fails is sent again, and each part has a length.
func TestFilesUploadResumes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DEPLOYYY_TOKEN", "token")
	pollEvery = time.Millisecond
	content := []byte("0123456789abcdefghij")
	local := filepath.Join(t.TempDir(), "shops.sql.gz")
	os.WriteFile(local, content, 0o600)
	files := &fakeFiles{t: t, partSize: 6, size: int64(len(content)), phase: "Uploading",
		// Part 1 is listed; part 2 reached the store after the last grant of a broken run.
		listed: []int{1}, stored: map[int][]byte{1: content[:6], 2: content[6:12]}, failOnce: map[int]bool{4: true}}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Setenv("DEPLOYYY_API", server.URL)
	files.serve(mux, func() string { return server.URL })

	output, err := run(t, "", "files", "upload", local, "--project", "acme/shop", "--path", "imports")
	if err != nil || output != "file-1\n" {
		t.Fatalf("upload: %v, output %q", err, output)
	}
	block := sha256.Sum256(content)
	want := sha256.Sum256(block[:])
	if files.input["fileName"] != "imports/shops.sql.gz" || files.input["fingerprint"] != hex.EncodeToString(want[:]) || files.input["projectId"] != "project-1" {
		t.Errorf("start: %v", files.input)
	}
	slices.Sort(files.puts)
	if !slices.Equal(files.puts, []int{3, 4}) {
		t.Errorf("parts sent: %v, want 3 and 4 once", files.puts)
	}
	if got := bytes.Join([][]byte{files.stored[1], files.stored[2], files.stored[3], files.stored[4]}, nil); !bytes.Equal(got, content) || files.phase != "Ready" {
		t.Errorf("stored %q, phase %s", got, files.phase)
	}
}

// An import into the running database uploads a local file, names the choice, and follows the change until it ends.
func TestDatabaseImportIntoTheRunningDatabase(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DEPLOYYY_TOKEN", "token")
	pollEvery = time.Millisecond
	local := filepath.Join(t.TempDir(), "shop_9999.sql.gz")
	os.WriteFile(local, []byte("dump"), 0o600)
	var session, created map[string]any
	refused, polls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string
			Variables map[string]any
		}
		json.NewDecoder(r.Body).Decode(&request)
		switch {
		case strings.Contains(request.Query, "environments(filter"):
			io.WriteString(w, `{"data":{"viewer":{"teams":{"nodes":[{"projects":{"nodes":[{"id":"project-1","apps":{"nodes":[{"environments":{"nodes":[{"id":"environment-1","level":"PREVIEW","status":{"services":[{"kind":"CACHE","engine":"redis-7"},{"kind":"DATABASE","engine":"percona-8.0.36"}]}}]}}]}}]}}]}}}}`)
		case strings.Contains(request.Query, "projectFileUploadStart"):
			if name := request.Variables["input"].(map[string]any)["fileName"]; name != "imports/shop_9999.sql.gz" {
				t.Errorf("file name %v", name)
			}
			io.WriteString(w, `{"data":{"projectFileUploadStart":{"file":{"id":"file-1","status":{"phase":"Ready"}}}}}`)
		case strings.Contains(request.Query, "environmentDatabaseImportSessionCreate"):
			session = request.Variables["input"].(map[string]any)
			io.WriteString(w, `{"data":{"environmentDatabaseImportSessionCreate":{"session":{"metadata":{"namespace":"shop-preview-1","name":"import-1"}}}}}`)
		case strings.Contains(request.Query, "environmentDatabaseImportCreate"):
			if refused == 0 {
				refused++
				io.WriteString(w, `{"errors":[{"message":"Select a stored archive for this environment first."}]}`)
				return
			}
			created = request.Variables["input"].(map[string]any)
			io.WriteString(w, `{"data":{"environmentDatabaseImportCreate":{"change":{"id":"gid://deployyy/DatabaseImport/shop-preview-1/database-import-1"}}}}`)
		case strings.Contains(request.Query, "on DatabaseImport"):
			polls++
			phase, message := "RUNNING", "Loading 2 databases"
			if polls > 2 {
				phase, message = "SUCCEEDED", "The file is loaded into the running database of preview-1."
			}
			fmt.Fprintf(w, `{"data":{"node":{"status":{"phase":%q,"message":%q,"noticed":[{"key":"backup","value":"before-import-1"}],"steps":[{"progress":{"done":50,"of":100,"unit":"rows"}}]},"actions":{"command":{"allowed":false}}}}}`, phase, message)
		default:
			t.Errorf("unexpected query %s", request.Query)
		}
	}))
	defer server.Close()
	t.Setenv("DEPLOYYY_API", server.URL)

	output, err := run(t, "", "database", "import", local, "--environment", "acme/shop/laravel/preview-1", "--into-running", "--database", "shop_9999", "--no-backup")
	if err != nil || output != "The file is loaded into the running database of preview-1.\n" {
		t.Fatalf("import: %v, output %q", err, output)
	}
	if session["engine"] != "PERCONA" || session["fileId"] != "file-1" || session["environmentId"] != "environment-1" {
		t.Errorf("session: %v", session)
	}
	if created["sessionId"] != "gid://deployyy/DatabaseImportSession/shop-preview-1/import-1" || created["into"] != "RUNNING_DATABASE" || created["database"] != "shop_9999" || created["skipBackup"] != true || created["replace"] != true {
		t.Errorf("import: %v", created)
	}

	if _, err := run(t, "", "database", "import", local, "--environment", "acme/shop/laravel/preview-1", "--database", "shop_9999"); err == nil || !strings.Contains(err.Error(), "--into-running") {
		t.Errorf("a database name without --into-running: %v", err)
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

// fakeRclone serves a release archive of rclone for each platform: a script that writes its arguments and the endpoint of its remote, and logs the stats of a copy of 2 files with 1 failed file.
func fakeRclone(t *testing.T, mux *http.ServeMux, record string) {
	t.Helper()
	script := `#!/bin/sh
printf '%s\n' "$@" > ` + record + `
echo "$RCLONE_CONFIG_DEPLOYYY_ENDPOINT $RCLONE_CONFIG_DEPLOYYY_ACCESS_KEY_ID" >> ` + record + `
echo '{"level":"error","msg":"Failed to copy: denied","object":"b/broken.jpg"}' >&2
echo '{"level":"notice","msg":"stats","stats":{"bytes":2048,"totalBytes":4096,"transfers":2,"totalTransfers":3,"errors":1,"speed":1024,"eta":2}}' >&2
exit 1
`
	for platform := range rcloneSums {
		var archive bytes.Buffer
		files := zip.NewWriter(&archive)
		entry, _ := files.CreateHeader(&zip.FileHeader{Name: "rclone-" + rcloneVersion + "-" + platform + "/rclone"})
		io.WriteString(entry, script)
		files.Close()
		sum := sha256.Sum256(archive.Bytes())
		rcloneSums[platform] = hex.EncodeToString(sum[:])
		body := archive.Bytes()
		mux.HandleFunc("GET /rclone/"+rcloneVersion+"/rclone-"+rcloneVersion+"-"+platform+".zip", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	}
}

// transferAPI stubs the API of a transfer and the downloads of rclone. It returns the file of the arguments of rclone, the start and the updates.
func transferAPI(t *testing.T) (string, *map[string]any, *[]map[string]any) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DEPLOYYY_TOKEN", "token")
	pollEvery = time.Millisecond
	sums := maps.Clone(rcloneSums)
	t.Cleanup(func() { rcloneSums = sums })
	record := filepath.Join(t.TempDir(), "rclone.args")
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	t.Setenv("DEPLOYYY_API", server.URL)
	rcloneDownloads = server.URL + "/rclone"
	fakeRclone(t, mux, record)
	var started map[string]any
	var updates []map[string]any
	connects := 0
	mux.HandleFunc("POST /api/graphql", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string
			Variables map[string]any
		}
		json.NewDecoder(r.Body).Decode(&request)
		reply := func(data any) { json.NewEncoder(w).Encode(map[string]any{"data": data}) }
		transfer := map[string]any{"id": "transfer-1", "status": map[string]any{"phase": "PENDING"}}
		switch {
		case strings.Contains(request.Query, "projects(filter"):
			io.WriteString(w, `{"data":{"viewer":{"teams":{"nodes":[{"projects":{"nodes":[{"id":"project-1"}]}}]}}}}`)
		case strings.Contains(request.Query, "fileTransferStart"):
			started = request.Variables["input"].(map[string]any)
			reply(map[string]any{"fileTransferStart": map[string]any{"transfer": transfer}})
		case strings.Contains(request.Query, "fileTransferConnect"):
			connects++
			payload := map[string]any{"transfer": transfer}
			if connects > 1 {
				payload["connection"] = map[string]any{"endpoint": "https://t--transfer.acme.deployyy.app", "bucket": "data",
					"accessKeyId": "AKIAAAAAAAAAAAAAAAAA", "secretAccessKey": "secret", "expiresAt": "2026-11-24T10:00:00Z"}
			}
			reply(map[string]any{"fileTransferConnect": payload})
		case strings.Contains(request.Query, "fileTransferUpdate"):
			updates = append(updates, request.Variables["input"].(map[string]any))
			reply(map[string]any{"fileTransferUpdate": map[string]any{"transfer": transfer}})
		default:
			t.Errorf("unexpected query %s", request.Query)
		}
	})
	return record, &started, &updates
}

// An upload of a folder to a location starts a transfer, runs rclone on its endpoint, reports the failed file and ends the transfer.
func TestFilesUploadCopiesThroughATransfer(t *testing.T) {
	record, started, updates := transferAPI(t)
	source := t.TempDir()
	_, err := run(t, "", "files", "upload", source, "images:storage/app/beeld/", "--project", "acme/shop", "--transfers", "8")
	if err == nil || err.Error() != "1 file failed. Run the same command again to copy them" {
		t.Fatalf("upload: %v", err)
	}
	if (*started)["projectId"] != "project-1" || (*started)["location"] != "images" || (*started)["access"] != "WRITE" {
		t.Errorf("start: %v", *started)
	}
	args, _ := os.ReadFile(record)
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if lines[0] != "copy" || lines[1] != source || lines[2] != "deployyy:data/storage/app/beeld" || !slices.Contains(lines, "8") {
		t.Errorf("rclone ran with %q", lines)
	}
	if lines[len(lines)-1] != "https://t--transfer.acme.deployyy.app AKIAAAAAAAAAAAAAAAAA" {
		t.Errorf("the remote is the endpoint of the transfer: %q", lines[len(lines)-1])
	}
	last := (*updates)[len(*updates)-1]
	report, _ := last["report"].(map[string]any)
	if last["end"] != true || report["failedFiles"] != float64(1) || report["bytes"] != float64(2048) || fmt.Sprint(report["failed"]) != "[b/broken.jpg]" {
		t.Errorf("the transfer ends with the report: %v", last)
	}
}

// --from runs rclone on the server of the person with the keys in a temporary folder, and removes the folder.
func TestFilesUploadFromAServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	record, _, updates := transferAPI(t)
	// ssh runs the command of the server on this machine.
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "ssh"), []byte("#!/bin/sh\nwhile [ \"$1\" = \"-o\" ]; do shift 2; done\nshift\nexec sh -c \"$1\"\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", t.TempDir())
	_, err := run(t, "", "files", "upload", "--from", "deploy@shop.example.com:/data/beeld", "images:storage/app/beeld", "--project", "acme/shop", "--delete")
	if err == nil || !strings.Contains(err.Error(), "1 file failed") {
		t.Fatalf("upload: %v", err)
	}
	args, _ := os.ReadFile(record)
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if lines[0] != "--config" || !strings.HasSuffix(lines[1], "/rclone.conf") || lines[2] != "sync" || lines[3] != "/data/beeld" {
		t.Errorf("rclone ran on the server with %q", lines)
	}
	if entries, _ := os.ReadDir(os.Getenv("TMPDIR")); len(entries) != 0 {
		t.Errorf("the folder of rclone and the keys stays on the server: %v", entries)
	}
	if last := (*updates)[len(*updates)-1]; last["end"] != true {
		t.Errorf("the transfer ends: %v", last)
	}
}

func TestLocationPath(t *testing.T) {
	for argument, want := range map[string]string{
		"images:storage/app/beeld/": "images storage/app/beeld true",
		"files@rc:":                 "files@rc  true",
		"./beeld":                   "  false",
		"/var/www/a:b":              "  false",
	} {
		location, folder, ok := locationPath(argument)
		if got := fmt.Sprint(location, " ", folder, " ", ok); got != want {
			t.Errorf("%s: %q, want %q", argument, got, want)
		}
	}
}
