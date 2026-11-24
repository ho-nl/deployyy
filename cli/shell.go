package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strconv"
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
		Long:  "Opens a shell in a copy of the environment, with the build, the variables and the shared files of the environment. With commands on standard input, the shell runs the commands, prints their output and exits with the status of the last command. The copy stays for 15 minutes after the terminal or the script disconnects, so the next commands start at once.",
		Example: `  deployyy shell acme/shop/magento/main
  echo 'bin/magento indexer:status' | deployyy shell acme/shop/magento/main`,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			ctx := command.Context()
			scope := strings.SplitN(args[0], "/", 4)
			if len(scope) != 4 {
				return errors.New("Name the environment as <team>/<project>/<app>/<branch>")
			}
			interactive := term.IsTerminal(int(os.Stdin.Fd()))
			var script []byte
			if !interactive {
				var err error
				if script, err = io.ReadAll(os.Stdin); err != nil {
					return err
				}
				if len(bytes.TrimSpace(script)) == 0 {
					return errors.New("Standard input has no commands")
				}
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

			if interactive && environment.Level == "PROD" {
				fmt.Fprintf(os.Stderr, "%s is production.\n", args[0])
			}

			created, err := query[struct {
				EnvironmentShellCreate struct{ ChangeID string }
			}](ctx, `mutation ($input: EnvironmentShellCreateInput!) {
  environmentShellCreate(input: $input) {
    changeId
  }
}`, map[string]any{"input": map[string]any{
				"environmentId":  environment.ID,
				"idempotencyKey": rand.Text(),
				"shell":          shellInput(instance),
			}})
			if err != nil {
				return err
			}
			change := created.EnvironmentShellCreate.ChangeID

			said := ""
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
			// Each local terminal window has its own terminal in the shell, and each script types in the terminal `script`.
			terminal := "script"
			if interactive {
				var window syscall.Stat_t
				if err := syscall.Fstat(int(os.Stdin.Fd()), &window); err != nil {
					return err
				}
				host, _ := os.Hostname()
				sum := sha256.Sum256(fmt.Appendf(nil, "%s %d", host, window.Rdev))
				terminal = "cli-" + hex.EncodeToString(sum[:4])
			}
			socket, _, err := websocket.Dial(ctx, attached.EnvironmentShellAttach.URL+"?ticket="+url.QueryEscape(attached.EnvironmentShellAttach.Ticket)+"&terminal="+terminal, nil)
			if err != nil {
				return err
			}
			defer socket.CloseNow()
			socket.SetReadLimit(-1)

			// A text frame is a message of the terminal service, as JSON or as plain text.
			say := func(data []byte) {
				var note struct{ Message string }
				if json.Unmarshal(data, &note) != nil || note.Message == "" {
					note.Message = string(data)
				}
				fmt.Fprintf(os.Stderr, "\r\n%s\r\n", note.Message)
			}

			if !interactive {
				// The typed lines turn the echo off and run the script in one eval. The output between the two markers is the output of the script alone.
				// PAGER=cat: psql, git and systemctl print to the script instead of a pager that waits for a key.
				marker := "__deployyy_" + rand.Text() + "_"
				typed := " stty -echo\n export PAGER=cat\n __deployyy=\n"
				for encoded := base64.StdEncoding.EncodeToString(script); encoded != ""; {
					chunk := min(len(encoded), 512)
					typed += " __deployyy=${__deployyy}" + encoded[:chunk] + "\n"
					encoded = encoded[chunk:]
				}
				// Each marker is an operating system command of the terminal, which a screen that shows the shell does not print. The typed text holds the escape as `\033`, so an echo of the line matches no marker.
				typed += fmt.Sprintf(" printf '\\033]7770;%[1]sstart\\007'; eval \"$(printf %%s \"$__deployyy\" | base64 -d)\"; printf '\\033]7770;%[1]send_%%d\\007' \"$?\"; unset __deployyy; stty echo\n", marker)
				marker = "\x1b]7770;" + marker
				start, end := []byte(marker+"start\x07"), regexp.MustCompile(regexp.QuoteMeta(marker)+`end_(\d+)\x07`)
				partial := regexp.MustCompile(`^` + regexp.QuoteMeta(marker) + `end_\d*$`)

				// Ctrl-C interrupts the script and leaves the shell for the next script.
				reading := context.WithoutCancel(ctx)
				defer context.AfterFunc(ctx, func() {
					_ = socket.Write(reading, websocket.MessageBinary, []byte{3})
					_ = socket.Close(websocket.StatusNormalClosure, "")
				})()

				var output []byte
				sent, started := false, false
				for {
					kind, data, err := socket.Read(reading)
					if err != nil {
						var closed websocket.CloseError
						switch {
						case ctx.Err() != nil:
							return ctx.Err()
						case !errors.As(err, &closed):
							return errors.New("The connection to the shell was lost")
						case closed.Code != closeExited:
							return fmt.Errorf("The shell closed. %s", closed.Reason)
						}
						// The script ended the shell.
						if started {
							os.Stdout.Write(bytes.ReplaceAll(output, []byte("\r\n"), []byte("\n")))
						}
						return nil
					}
					if kind == websocket.MessageText {
						say(data)
						continue
					}
					// The first output is the prompt: the shell reads its input.
					if !sent {
						sent = true
						go func() {
							for rest := []byte(typed); len(rest) > 0; {
								frame := min(len(rest), 32*1024)
								if socket.Write(reading, websocket.MessageBinary, rest[:frame]) != nil {
									return
								}
								rest = rest[frame:]
							}
						}()
					}
					output = append(output, data...)
					if !started {
						at := bytes.Index(output, start)
						if at < 0 {
							output = output[max(0, len(output)-len(start)):]
							continue
						}
						output, started = output[at+len(start):], true
					}
					// A tail that is the begin of the end marker or half a line end waits for the next frame.
					done := end.FindSubmatchIndex(output)
					printed := len(output)
					if done != nil {
						printed = done[0]
					} else {
						for held := min(len(output), len(marker)+16); held > 0; held-- {
							tail := string(output[len(output)-held:])
							if strings.HasPrefix(marker+"end_", tail) || partial.MatchString(tail) {
								printed = len(output) - held
								break
							}
						}
						for printed > 0 && output[printed-1] == '\r' {
							printed--
						}
					}
					os.Stdout.Write(bytes.ReplaceAll(output[:printed], []byte("\r\n"), []byte("\n")))
					if done != nil {
						status, _ := strconv.Atoi(string(output[done[2]:done[3]]))
						_ = socket.Close(websocket.StatusNormalClosure, "")
						if status != 0 {
							return exitStatus(status)
						}
						return nil
					}
					output = output[printed:]
				}
			}

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
			go func() {
				buffer := make([]byte, 32*1024)
				for {
					read, err := os.Stdin.Read(buffer)
					if err != nil || socket.Write(ctx, websocket.MessageBinary, buffer[:read]) != nil {
						return
					}
				}
			}()

			for {
				kind, data, err := socket.Read(ctx)
				if err != nil {
					var closed websocket.CloseError
					if !errors.As(err, &closed) {
						return errors.New("\nThe connection to the shell was lost")
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
				say(data)
			}
		},
	}
	command.Flags().StringVar(&instance, "instance", "", "open the shell on a running instance, by its name in Environment.instances")
	return command
}
