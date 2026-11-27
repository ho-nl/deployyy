package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The values of the GraphQL enum LogLevel.
var logLevels = []string{"DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL", "UNKNOWN"}

// The root field of each scope of a path <team>/<project>/<app>/<branch>, by the number of its parts.
var logScopes = []struct{ field, arguments, variables string }{
	{"team", "slug: $team", "$team: String!"},
	{"project", "team: $team, slug: $project", "$team: String!, $project: String!"},
	{"app", "team: $team, project: $project, slug: $app", "$team: String!, $project: String!, $app: String!"},
	{"environment", "team: $team, project: $project, app: $app, branch: $branch", "$team: String!, $project: String!, $app: String!, $branch: String!"},
}

const logLineFields = `at level service text environment { branch app { slug project { slug } } }`

type logLine struct {
	At, Level, Service, Text string
	Environment              *struct {
		Branch string
		App    struct {
			Slug    string
			Project struct{ Slug string }
		}
	}
}

type logEdge struct {
	Cursor string
	Node   json.RawMessage
}

func logsCommand() *cobra.Command {
	var follow, asJSON bool
	var levels, services []string
	var search, since string
	command := &cobra.Command{
		Use:   "logs <team>[/<project>[/<app>[/<branch>]]]",
		Short: "Print the log lines of an environment, or of each environment below a team, a project or an app",
		Long:  "Prints the log lines, oldest first: the time, the level, the environment below the scope, the source and the text. Without --since, the lines of the last hour. With --follow, the command prints each new line until Ctrl-C.",
		Example: `  deployyy logs acme/shop/magento/main --level error,critical
  deployyy logs acme/shop --since 24h --search "SQLSTATE" --service magento-web
  deployyy logs acme/shop/magento/main -f --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			path := strings.Split(args[0], "/")
			if len(path) > 4 || slices.Contains(path, "") {
				return errors.New("Name the scope as <team>, <team>/<project>, <team>/<project>/<app> or <team>/<project>/<app>/<branch>")
			}
			filter := map[string]any{}
			var picked []string
			for _, level := range levels {
				if level = strings.ToUpper(strings.TrimSpace(level)); level == "WARN" {
					level = "WARNING"
				}
				if !slices.Contains(logLevels, level) {
					return fmt.Errorf("Level %q is unknown. Use debug, info, warning, error, critical or unknown", level)
				}
				picked = append(picked, level)
			}
			if len(picked) > 0 {
				filter["levels"] = picked
			}
			if len(services) > 0 {
				filter["services"] = services
			}
			if search != "" {
				filter["search"] = search
			}
			if since != "" {
				if duration, err := time.ParseDuration(since); err == nil {
					filter["lastSeconds"] = int(duration.Seconds())
				} else if at, err := time.Parse(time.RFC3339, since); err == nil {
					filter["since"] = at.UTC().Format(time.RFC3339)
				} else {
					return fmt.Errorf("Write --since as a duration, such as 1h, or as a time, such as 2026-11-27T09:00:00Z")
				}
			}
			scope := logScopes[len(path)-1]
			values := map[string]any{"filter": filter}
			for index, name := range []string{"team", "project", "app", "branch"}[:len(path)] {
				values[name] = path[index]
			}
			print := logPrinter(len(path), asJSON, term.IsTerminal(int(os.Stdout.Fd())))
			ctx := command.Context()
			id, cursor, err := printLogPages(ctx, args[0], scope.field, scope.arguments, scope.variables, values, print)
			if err != nil || !follow {
				return err
			}
			return followLogs(ctx, id, cursor, filter, print)
		},
	}
	command.Flags().BoolVarP(&follow, "follow", "f", false, "print each new line until Ctrl-C")
	command.Flags().StringSliceVar(&levels, "level", nil, "the levels to print, such as error,warning; without the option, each level")
	command.Flags().StringArrayVar(&services, "service", nil, "a source to print, such as magento-web or database; repeat the option for more sources")
	command.Flags().StringVar(&search, "search", "", "the text that each line holds")
	command.Flags().StringVar(&since, "since", "", "the start of the lines, as a duration such as 24h or an RFC 3339 time; without the option, one hour ago")
	command.Flags().BoolVar(&asJSON, "json", false, "print each line as one JSON object")
	return command
}

// Prints each line of the scope that matches the filter, oldest first, and returns the ID of the scope and the cursor after the newest line.
func printLogPages(ctx context.Context, name, field, arguments, variables string, values map[string]any, print func(json.RawMessage) error) (string, string, error) {
	document := fmt.Sprintf(`query (%s, $filter: LogFilter, $after: String) {
  scope: %s(%s) {
    id
    allLogs(first: 1000, after: $after, filter: $filter) {
      liveCursor
      edges { cursor node { %s } }
      pageInfo { hasNextPage endCursor }
    }
  }
}`, variables, field, arguments, logLineFields)
	var pages [][]logEdge
	id, live := "", ""
	for after := ""; ; {
		if after != "" {
			values["after"] = after
		}
		read, err := query[struct {
			Scope *struct {
				ID      string
				AllLogs struct {
					LiveCursor string
					Edges      []logEdge
					PageInfo   struct {
						HasNextPage bool
						EndCursor   string
					}
				}
			}
		}](ctx, document, values)
		if err != nil {
			return "", "", err
		}
		if read.Scope == nil {
			return "", "", fmt.Errorf("No %s %s", field, name)
		}
		if id == "" {
			id, live = read.Scope.ID, read.Scope.AllLogs.LiveCursor
		}
		pages = append(pages, read.Scope.AllLogs.Edges)
		if !read.Scope.AllLogs.PageInfo.HasNextPage {
			break
		}
		after = read.Scope.AllLogs.PageInfo.EndCursor
	}
	// The API returns the newest line first.
	for page := len(pages) - 1; page >= 0; page-- {
		for edge := len(pages[page]) - 1; edge >= 0; edge-- {
			if err := print(pages[page][edge].Node); err != nil {
				return "", "", err
			}
		}
	}
	return id, live, nil
}

// Prints the new lines of the scope after cursor until Ctrl-C or the end of the stream. A RESET or a STREAM_REOPEN of the API starts the stream again after the last line.
func followLogs(ctx context.Context, id, cursor string, filter map[string]any, print func(json.RawMessage) error) error {
	document := fmt.Sprintf(`subscription ($id: ID!, $after: String, $filter: LogFilter) {
  logUpdates(scopeId: $id, after: $after, filter: $filter) {
    type
    cursor
    edges { cursor node { %s } }
  }
}`, logLineFields)
	for {
		answer, err := send(ctx, document, map[string]any{"id": id, "after": cursor, "filter": filter})
		if err != nil {
			return err
		}
		reopen, err := func() (bool, error) {
			defer answer.Body.Close()
			lines := bufio.NewScanner(answer.Body)
			lines.Buffer(nil, 16<<20)
			event := ""
			for lines.Scan() {
				field, value, _ := strings.Cut(lines.Text(), ":")
				value = strings.TrimPrefix(value, " ")
				if field == "event" {
					event = value
				}
				if field != "data" || event != "next" {
					continue
				}
				var update struct {
					Data *struct {
						LogUpdates struct {
							Type, Cursor string
							Edges        []logEdge
						}
					}
					Errors []struct {
						Message    string
						Extensions struct{ Code string }
					}
				}
				if err := json.Unmarshal([]byte(value), &update); err != nil {
					return false, fmt.Errorf("The API answered without JSON: %s", value)
				}
				if len(update.Errors) > 0 {
					return update.Errors[0].Extensions.Code == "STREAM_REOPEN", errors.New(update.Errors[0].Message)
				}
				if update.Data == nil {
					continue
				}
				for _, edge := range update.Data.LogUpdates.Edges {
					if err := print(edge.Node); err != nil {
						return false, err
					}
				}
				cursor = update.Data.LogUpdates.Cursor
				if update.Data.LogUpdates.Type == "RESET" {
					return true, nil
				}
			}
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, lines.Err()
		}()
		if !reopen {
			return err
		}
	}
}

// Returns the printer of one line of a scope with depth parts: one JSON object, or the time, the level, the environment below the scope, the source and the text.
func logPrinter(depth int, asJSON, colour bool) func(json.RawMessage) error {
	return func(node json.RawMessage) error {
		if asJSON {
			_, err := os.Stdout.Write(append(node, '\n'))
			return err
		}
		var line logLine
		if err := json.Unmarshal(node, &line); err != nil {
			return err
		}
		level := fmt.Sprintf("%-8s", line.Level)
		if colour {
			switch line.Level {
			case "ERROR", "CRITICAL":
				level = "\x1b[31m" + level + "\x1b[0m"
			case "WARNING":
				level = "\x1b[33m" + level + "\x1b[0m"
			case "DEBUG", "UNKNOWN":
				level = "\x1b[2m" + level + "\x1b[0m"
			}
		}
		parts := []string{line.At, level}
		if line.Environment != nil && depth < 4 {
			parts = append(parts, strings.Join([]string{line.Environment.App.Project.Slug, line.Environment.App.Slug, line.Environment.Branch}[depth-1:], "/"))
		}
		_, err := io.WriteString(os.Stdout, strings.Join(append(parts, line.Service, line.Text), " ")+"\n")
		return err
	}
}
