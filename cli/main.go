package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The release workflow sets the tag of the release.
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := rootCommand().ExecuteContext(ctx)
	stop()
	startUpdate()
	if errors.Is(err, context.Canceled) {
		os.Exit(130)
	}
	if status := exitStatus(0); errors.As(err, &status) {
		os.Exit(int(status))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// The exit status of a script that `shell` ran.
type exitStatus int

func (status exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(status)) }

func rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "deployyy",
		Short:         "Deployyy from the terminal",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(loginCommand(), logoutCommand(), graphqlCommand(), shellCommand(), logsCommand(), filesCommand(), databaseCommand(), updateCommand())
	return root
}

func graphqlCommand() *cobra.Command {
	var variables []string
	command := &cobra.Command{
		Use:   "graphql [document]",
		Short: "Run a GraphQL query, mutation or subscription",
		Long:  "Runs a GraphQL document on the API and prints the data and the errors of the response as JSON. A subscription prints each event as one line until the stream ends. Without an argument, the document comes from standard input.",
		Example: `  deployyy graphql '{ viewer { teams { nodes { slug } } } }'
  deployyy graphql 'query ($team: String!) { viewer { teams(filter: { slug: $team }) { nodes { id } } } }' -v team=acme
  deployyy graphql -v 'input={"environmentId":"…"}' < restart.graphql`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var document string
			if len(args) == 1 {
				document = args[0]
			} else if term.IsTerminal(int(os.Stdin.Fd())) {
				return command.Help()
			} else {
				input, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				document = string(input)
			}
			values := map[string]any{}
			for _, variable := range variables {
				name, value, found := strings.Cut(variable, "=")
				if !found {
					return fmt.Errorf("Variable %q has no value. Write name=value", variable)
				}
				// A value that is no JSON is a string.
				var parsed any
				if json.Unmarshal([]byte(value), &parsed) != nil {
					parsed = value
				}
				values[name] = parsed
			}
			ctx := command.Context()
			answer, err := send(ctx, document, values)
			if err != nil {
				return err
			}
			defer answer.Body.Close()
			// The gateway adds its diagnostics as `extensions`. The command prints the data and the errors.
			failed := false
			print := func(body []byte, format func(any) ([]byte, error)) error {
				var response struct {
					Data   json.RawMessage   `json:"data,omitempty"`
					Errors []json.RawMessage `json:"errors,omitempty"`
				}
				if err := json.Unmarshal(body, &response); err != nil {
					return fmt.Errorf("The API answered without JSON: %s", strings.TrimSpace(string(body)))
				}
				output, err := format(response)
				if err != nil {
					return err
				}
				os.Stdout.Write(append(output, '\n'))
				failed = failed || len(response.Errors) > 0
				return nil
			}
			if strings.HasPrefix(answer.Header.Get("Content-Type"), "text/event-stream") {
				// A subscription: each `next` event prints as one line, until the API ends the stream or Ctrl-C.
				lines := bufio.NewScanner(answer.Body)
				lines.Buffer(nil, 16<<20)
				event := ""
				for lines.Scan() {
					field, value, _ := strings.Cut(lines.Text(), ":")
					value = strings.TrimPrefix(value, " ")
					if field == "event" {
						event = value
					}
					if field == "data" && event == "next" {
						if err := print([]byte(value), json.Marshal); err != nil {
							return err
						}
					}
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err := lines.Err(); err != nil {
					return err
				}
			} else {
				body, err := io.ReadAll(answer.Body)
				if err != nil {
					return err
				}
				if err := print(body, func(response any) ([]byte, error) { return json.MarshalIndent(response, "", "  ") }); err != nil {
					return err
				}
			}
			if failed {
				return errors.New("The response has errors")
			}
			return nil
		},
	}
	command.Flags().StringArrayVarP(&variables, "variable", "v", nil, "a variable of the document as name=value; a JSON value keeps its type")
	return command
}
