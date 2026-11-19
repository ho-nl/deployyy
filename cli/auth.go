package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

// The sign-in of a person at one console, as `login` saves it.
type account struct {
	ClientID string `json:"client_id"`
	// RFC 7592: the address and the token that delete the client.
	RegistrationURI   string        `json:"registration_client_uri"`
	RegistrationToken string        `json:"registration_access_token"`
	TokenURL          string        `json:"token_endpoint"`
	RevocationURL     string        `json:"revocation_endpoint"`
	Token             *oauth2.Token `json:"token"`
}

// The browser of the person. A test replaces it.
var openBrowser = func(address string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	_ = exec.Command(name, address).Start()
}

func apiOrigin() string {
	if origin := os.Getenv("DEPLOYYY_API"); origin != "" {
		return strings.TrimRight(origin, "/")
	}
	return "https://deployyy.com"
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "deployyy")
	return dir, os.MkdirAll(dir, 0o700)
}

// Runs change on the saved accounts, by console origin, and saves the result. A lock keeps one process at a time in change: Zitadel accepts a refresh token once.
func withAccounts(change func(accounts map[string]*account) error) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	file := filepath.Join(dir, "accounts.json")
	accounts := map[string]*account{}
	saved, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(saved, &accounts)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := change(accounts); err != nil {
		return err
	}
	result, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil || bytes.Equal(result, saved) {
		return err
	}
	return os.WriteFile(file, result, 0o600)
}

func getJSON(ctx context.Context, address string, result any) error {
	body, err := download(ctx, address)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, result)
}

func loginCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Sign in through the browser",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			ctx := command.Context()
			origin := apiOrigin()

			// RFC 9728: the console names the issuer and the scopes of its API.
			var resource struct {
				AuthorizationServers []string `json:"authorization_servers"`
				ScopesSupported      []string `json:"scopes_supported"`
			}
			if err := getJSON(ctx, origin+"/.well-known/oauth-protected-resource/api/graphql", &resource); err != nil {
				return err
			}
			if len(resource.AuthorizationServers) != 1 {
				return fmt.Errorf("%s names no sign-in service", origin)
			}
			var issuer struct {
				AuthorizationEndpoint string `json:"authorization_endpoint"`
				TokenEndpoint         string `json:"token_endpoint"`
				RegistrationEndpoint  string `json:"registration_endpoint"`
				RevocationEndpoint    string `json:"revocation_endpoint"`
			}
			if err := getJSON(ctx, strings.TrimRight(resource.AuthorizationServers[0], "/")+"/.well-known/openid-configuration", &issuer); err != nil {
				return err
			}

			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return err
			}
			defer listener.Close()

			// The client stays saved when the sign-in fails, and the next sign-in uses it.
			var saved account
			err = withAccounts(func(accounts map[string]*account) error {
				if known := accounts[origin]; known != nil && known.TokenURL == issuer.TokenEndpoint {
					saved = *known
					return nil
				}
				// RFC 8252: the issuer accepts each port of the loopback address.
				registration, err := json.Marshal(map[string]any{
					"client_name":                "Deployyy CLI",
					"application_type":           "native",
					"redirect_uris":              []string{"http://127.0.0.1/callback"},
					"grant_types":                []string{"authorization_code", "refresh_token"},
					"response_types":             []string{"code"},
					"token_endpoint_auth_method": "none",
				})
				if err != nil {
					return err
				}
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, issuer.RegistrationEndpoint, bytes.NewReader(registration))
				if err != nil {
					return err
				}
				request.Header.Set("Content-Type", "application/json")
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					return err
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
					return fmt.Errorf("%s answered %s", issuer.RegistrationEndpoint, response.Status)
				}
				saved = account{TokenURL: issuer.TokenEndpoint, RevocationURL: issuer.RevocationEndpoint}
				accounts[origin] = &saved
				return json.NewDecoder(response.Body).Decode(&saved)
			})
			if err != nil {
				return err
			}

			client := oauth2.Config{
				ClientID:    saved.ClientID,
				Endpoint:    oauth2.Endpoint{AuthURL: issuer.AuthorizationEndpoint, TokenURL: issuer.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams},
				RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port),
				Scopes:      resource.ScopesSupported,
			}
			state, verifier := rand.Text(), oauth2.GenerateVerifier()
			type outcome struct {
				token *oauth2.Token
				err   error
			}
			done := make(chan outcome, 1)
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if r.URL.Path != "/callback" || query.Get("state") != state {
					http.NotFound(w, r)
					return
				}
				var result outcome
				if failure := query.Get("error"); failure != "" {
					result.err = fmt.Errorf("The sign-in failed: %s %s", failure, query.Get("error_description"))
				} else {
					result.token, result.err = client.Exchange(r.Context(), query.Get("code"), oauth2.VerifierOption(verifier))
				}
				if result.err != nil {
					http.Error(w, result.err.Error(), http.StatusBadRequest)
				} else {
					fmt.Fprintln(w, "Signed in. Close this tab.")
				}
				done <- result
			})}
			go server.Serve(listener)
			defer server.Close()

			address := client.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
			fmt.Fprintf(os.Stderr, "Sign in at:\n\n  %s\n\n", address)
			openBrowser(address)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case result := <-done:
				if result.err != nil {
					return result.err
				}
				saved.Token = result.token
			}
			if err := withAccounts(func(accounts map[string]*account) error {
				accounts[origin] = &saved
				return nil
			}); err != nil {
				return err
			}
			data, err := query[struct {
				Viewer *struct{ PreferredLoginName string }
			}](ctx, `{ viewer { preferredLoginName } }`, nil)
			if err != nil {
				return err
			}
			if data.Viewer == nil {
				return fmt.Errorf("%s refused the sign-in", origin)
			}
			fmt.Fprintf(os.Stderr, "Signed in at %s as %s\n", origin, data.Viewer.PreferredLoginName)
			return nil
		},
	}
}

func logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out and delete the saved sign-in",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			origin := apiOrigin()
			return withAccounts(func(accounts map[string]*account) error {
				saved := accounts[origin]
				if saved == nil {
					return nil
				}
				if saved.Token != nil && saved.Token.RefreshToken != "" {
					response, err := http.PostForm(saved.RevocationURL, url.Values{"token": {saved.Token.RefreshToken}, "client_id": {saved.ClientID}})
					if err != nil {
						return err
					}
					response.Body.Close()
				}
				request, err := http.NewRequestWithContext(command.Context(), http.MethodDelete, saved.RegistrationURI, nil)
				if err != nil {
					return err
				}
				request.Header.Set("Authorization", "Bearer "+saved.RegistrationToken)
				response, err := http.DefaultClient.Do(request)
				if err != nil {
					return err
				}
				response.Body.Close()
				delete(accounts, origin)
				fmt.Fprintf(os.Stderr, "Signed out at %s\n", origin)
				return nil
			})
		},
	}
}

// Sends a GraphQL document to the API as the signed-in person. The API answers a subscription with server-sent events. The caller closes the body.
func send(ctx context.Context, document string, variables map[string]any) (*http.Response, error) {
	origin := apiOrigin()
	token := os.Getenv("DEPLOYYY_TOKEN")
	if token == "" {
		err := withAccounts(func(accounts map[string]*account) error {
			saved := accounts[origin]
			if saved == nil || saved.Token == nil {
				return fmt.Errorf("Not signed in at %s. Run: deployyy login", origin)
			}
			client := oauth2.Config{ClientID: saved.ClientID, Endpoint: oauth2.Endpoint{TokenURL: saved.TokenURL, AuthStyle: oauth2.AuthStyleInParams}}
			fresh, err := client.TokenSource(ctx, saved.Token).Token()
			if refused := new(oauth2.RetrieveError); errors.As(err, &refused) {
				return fmt.Errorf("The sign-in at %s ended. Run: deployyy login", origin)
			}
			if err != nil {
				return err
			}
			saved.Token, token = fresh, fresh.AccessToken
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	body, err := json.Marshal(map[string]any{"query": document, "variables": variables})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(request)
}

// Runs a GraphQL document and returns its data. Each error of the response fails the call.
func query[Data any](ctx context.Context, document string, variables map[string]any) (Data, error) {
	var response struct {
		Data   Data
		Errors []struct{ Message string }
	}
	answer, err := send(ctx, document, variables)
	if err != nil {
		return response.Data, err
	}
	defer answer.Body.Close()
	body, err := io.ReadAll(answer.Body)
	if err != nil {
		return response.Data, err
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return response.Data, fmt.Errorf("The API answered without JSON: %s", strings.TrimSpace(string(body)))
	}
	if len(response.Errors) > 0 {
		return response.Data, errors.New(response.Errors[0].Message)
	}
	return response.Data, nil
}
