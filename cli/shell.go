package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type ability struct {
	Allowed bool
	Message string
}

// The terminal service closes with this code when the remote shell exits.
const closeExited websocket.StatusCode = 4001

// The shell opens on a copy of the environment, or on the running instance with this name.
func shellInput(instance string) any {
	if instance == "" {
		return nil
	}
	return map[string]string{"instance": instance}
}

func shellCommand() *cobra.Command {
	var instance string
	command := &cobra.Command{
		Use:   "shell <team>/<project>/<app>/<branch>",
		Short: "Open a shell in an environment",
		Long:  "Opens a shell in a copy of the environment, with the build, the variables and the shared files of the environment. A shell on the same copy opens again until the copy ends.",
		Example: `  deployyy shell acme/shop/magento/main
  echo 'bin/magento indexer:status' | deployyy shell acme/shop/magento/main`,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			ctx := command.Context()
			scope := strings.SplitN(args[0], "/", 4)
			if len(scope) != 4 {
				return errors.New("Name the environment as <team>/<project>/<app>/<branch>")
			}

			found, err := query[struct {
				Viewer *struct {
					Teams struct {
						Nodes []struct {
							Projects struct {
								Nodes []struct {
									Apps struct {
										Nodes []struct {
											Environments struct {
												Nodes []struct {
													ID      string
													Level   string
													Actions struct{ ShellCreate ability }
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}](ctx, `query ($team: String!, $project: String!, $app: String!, $branch: String!, $shell: EnvironmentShellInput) {
  viewer {
    teams(filter: { slug: $team }) {
      nodes {
        projects(filter: { slug: $project, state: ACTIVE }, first: 1, includeShared: true) {
          nodes {
            apps(filter: { slug: $app }, first: 1) {
              nodes {
                environments(filter: { branch: $branch }, first: 1) {
                  nodes {
                    id
                    level
                    actions {
                      shellCreate(input: $shell) {
                        allowed
                        message
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`, map[string]any{"team": scope[0], "project": scope[1], "app": scope[2], "branch": scope[3], "shell": shellInput(instance)})
			if err != nil {
				return err
			}
			if found.Viewer == nil {
				return fmt.Errorf("Not signed in at %s. Run: deployyy login", apiOrigin())
			}
			if len(found.Viewer.Teams.Nodes) == 0 ||
				len(found.Viewer.Teams.Nodes[0].Projects.Nodes) == 0 ||
				len(found.Viewer.Teams.Nodes[0].Projects.Nodes[0].Apps.Nodes) == 0 ||
				len(found.Viewer.Teams.Nodes[0].Projects.Nodes[0].Apps.Nodes[0].Environments.Nodes) == 0 {
				return fmt.Errorf("No environment %s", args[0])
			}
			environment := found.Viewer.Teams.Nodes[0].Projects.Nodes[0].Apps.Nodes[0].Environments.Nodes[0]
			if !environment.Actions.ShellCreate.Allowed {
				return errors.New(environment.Actions.ShellCreate.Message)
			}

			interactive := term.IsTerminal(int(os.Stdin.Fd()))
			production := environment.Level == "PROD"
			if production {
				if !interactive {
					return errors.New("A shell on production needs a confirmation in a terminal")
				}
				fmt.Fprintf(os.Stderr, "%s is production. Type yes to open the shell: ", args[0])
				answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if strings.TrimSpace(answer) != "yes" {
					return errors.New("No shell opened")
				}
			}

			created, err := query[struct {
				EnvironmentShellCreate struct{ ChangeID string }
			}](ctx, `mutation ($input: EnvironmentShellCreateInput!) {
  environmentShellCreate(input: $input) {
    changeId
  }
}`, map[string]any{"input": map[string]any{
				"environmentId":     environment.ID,
				"idempotencyKey":    rand.Text(),
				"shell":             shellInput(instance),
				"confirmProduction": production,
			}})
			if err != nil {
				return err
			}
			change := created.EnvironmentShellCreate.ChangeID

			said := "Starting the shell"
			fmt.Fprintln(os.Stderr, said)
			for {
				shell, err := query[struct {
					Node *struct {
						Status  *struct{ Phase, Message string }
						Actions *struct{ ShellAttach ability }
					}
				}](ctx, `query ($id: ID!) {
  node(id: $id) {
    ... on Change {
      status {
        phase
        message
      }
    }
    ... on CommandRun {
      actions {
        shellAttach {
          allowed
        }
      }
    }
  }
}`, map[string]any{"id": change})
				if err != nil {
					return err
				}
				if shell.Node == nil {
					return errors.New("The shell is gone")
				}
				if shell.Node.Actions != nil && shell.Node.Actions.ShellAttach.Allowed {
					break
				}
				if status := shell.Node.Status; status != nil {
					if slices.Contains([]string{"SUCCEEDED", "FAILED", "STOPPED", "CANCELLED", "REFUSED"}, status.Phase) {
						return fmt.Errorf("The shell ended. %s", status.Message)
					}
					if status.Message != "" && status.Message != said {
						said = status.Message
						fmt.Fprintln(os.Stderr, said)
					}
				}
				select {
				case <-ctx.Done():
					// A stopped wait cancels the shell, so no copy starts later.
					if _, err := query[struct{}](context.WithoutCancel(ctx), `mutation ($input: EnvironmentChangeActionInput!) {
  environmentChangeCancel(input: $input) {
    changeId
  }
}`, map[string]any{"input": map[string]any{"environmentId": environment.ID, "changeId": change}}); err != nil {
						return err
					}
					return ctx.Err()
				case <-time.After(time.Second):
				}
			}

			attached, err := query[struct {
				EnvironmentShellAttach struct{ URL, Ticket string }
			}](ctx, `mutation ($input: EnvironmentShellAttachInput!) {
  environmentShellAttach(input: $input) {
    url
    ticket
  }
}`, map[string]any{"input": map[string]any{"environmentId": environment.ID, "changeId": change}})
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			socket, _, err := websocket.Dial(ctx, attached.EnvironmentShellAttach.URL+"?ticket="+url.QueryEscape(attached.EnvironmentShellAttach.Ticket), nil)
			if err != nil {
				return err
			}
			defer socket.CloseNow()
			socket.SetReadLimit(-1)

			if interactive {
				saved, err := term.MakeRaw(int(os.Stdin.Fd()))
				if err != nil {
					return err
				}
				defer term.Restore(int(os.Stdin.Fd()), saved)
				resized := make(chan os.Signal, 1)
				signal.Notify(resized, syscall.SIGWINCH)
				defer signal.Stop(resized)
				resized <- syscall.SIGWINCH
				go func() {
					for range resized {
						columns, rows, err := term.GetSize(int(os.Stdout.Fd()))
						if err != nil {
							continue
						}
						frame, _ := json.Marshal(map[string]any{"type": "resize", "cols": columns, "rows": rows})
						_ = socket.Write(ctx, websocket.MessageText, frame)
					}
				}()
			}
			go func() {
				buffer := make([]byte, 32*1024)
				for {
					read, err := os.Stdin.Read(buffer)
					if read > 0 && socket.Write(ctx, websocket.MessageBinary, buffer[:read]) != nil {
						return
					}
					if err != nil {
						if err == io.EOF && !interactive {
							// End of transmission: the remote shell exits at the end of the input.
							_ = socket.Write(ctx, websocket.MessageBinary, []byte{4})
						}
						return
					}
				}
			}()

			for {
				kind, data, err := socket.Read(ctx)
				if err != nil {
					var closed websocket.CloseError
					if !errors.As(err, &closed) {
						return err
					}
					if closed.Code == closeExited || closed.Code == websocket.StatusNormalClosure {
						return nil
					}
					return fmt.Errorf("\nThe shell closed. %s", closed.Reason)
				}
				if kind == websocket.MessageBinary {
					os.Stdout.Write(data)
					continue
				}
				// A text frame is a message of the terminal service, as JSON or as plain text.
				var note struct{ Message string }
				if json.Unmarshal(data, &note) != nil || note.Message == "" {
					note.Message = string(data)
				}
				fmt.Fprintf(os.Stderr, "\r\n%s\r\n", note.Message)
			}
		},
	}
	command.Flags().StringVar(&instance, "instance", "", "open the shell on a running instance, by its name in Environment.instances")
	return command
}
