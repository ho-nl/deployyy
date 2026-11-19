package main

import (
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
	root.AddCommand(loginCommand(), logoutCommand(), graphqlCommand(), shellCommand(), updateCommand())
	return root
}

func graphqlCommand() *cobra.Command {
	var variables []string
	command := &cobra.Command{
		Use:   "graphql [document]",
		Short: "Run a GraphQL query or mutation",
		Long:  "Runs a GraphQL document on the API and prints the data and the errors of the response as JSON. Without an argument, the document comes from standard input.",
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
			body, err := post(command.Context(), document, values)
			if err != nil {
				return err
			}
			// The gateway adds its diagnostics as `extensions`. The command prints the data and the errors.
			var response struct {
				Data   json.RawMessage   `json:"data,omitempty"`
				Errors []json.RawMessage `json:"errors,omitempty"`
			}
			if err := json.Unmarshal(body, &response); err != nil {
				return fmt.Errorf("The API answered without JSON: %s", strings.TrimSpace(string(body)))
			}
			output, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				return err
			}
			os.Stdout.Write(append(output, '\n'))
			if len(response.Errors) > 0 {
				return errors.New("The response has errors")
			}
			return nil
		},
	}
	command.Flags().StringArrayVarP(&variables, "variable", "v", nil, "a variable of the document as name=value; a JSON value keeps its type")
	return command
}
