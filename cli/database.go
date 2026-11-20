package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func databaseCommand() *cobra.Command {
	database := &cobra.Command{Use: "database", Short: "The database of an environment"}
	var environment, name string
	var running, noBackup, freeze bool
	var variables []string
	load := &cobra.Command{
		Use:   "import <file>",
		Short: "Import a database file into an environment",
		Long: `Imports a .sql.gz file into the database of an environment and follows the import until it ends. A local file is uploaded to the folder imports of the Files area first. Any other argument names a stored file of the Files area by its path.

Without --into-running, the import fills a new database server, checks it and switches to it: the result equals the file, and the previous server is the rollback.

With --into-running, the import loads into the running database server: each database of the file is replaced, and each other database stays. A snapshot of the database disk before the load is the rollback. A file without database names loads into --database, or into the main database of the app.`,
		Example: `  deployyy database import shops.sql.gz --environment acme/shop/laravel/preview-1 --into-running
  deployyy database import imports/shop_9999.sql.gz --environment acme/shop/laravel/main --into-running --database shop_9999`,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			ctx := command.Context()
			if !running && (name != "" || noBackup) {
				return errors.New("--database and --no-backup take --into-running")
			}
			values := []map[string]string{}
			for _, variable := range variables {
				key, value, found := strings.Cut(variable, "=")
				if !found {
					return fmt.Errorf("Variable %q has no value. Write NAME=value", variable)
				}
				values = append(values, map[string]string{"name": key, "value": value})
			}
			target, err := findEnvironment(ctx, environment)
			if err != nil {
				return err
			}
			fileID := ""
			if info, err := os.Stat(args[0]); err == nil && info.Mode().IsRegular() {
				if fileID, err = uploadFile(ctx, target.OwnerProject.ID, args[0], path.Join("imports", filepath.Base(args[0]))); err != nil {
					return err
				}
			} else if fileID, err = storedFile(ctx, target.OwnerProject.ID, args[0]); err != nil {
				return err
			}

			engine := ""
			for _, service := range target.Status.Services {
				if service.Kind == "DATABASE" {
					for _, family := range []struct{ prefix, engine string }{{"mariadb", "MARIADB"}, {"percona", "PERCONA"}, {"mysql", "PERCONA"}, {"postgres", "POSTGRES"}} {
						if strings.HasPrefix(service.Engine, family.prefix) {
							engine = family.engine
						}
					}
				}
			}
			if engine == "" {
				return fmt.Errorf("Environment %s runs no database to import into", environment)
			}
			session, err := query[struct {
				EnvironmentDatabaseImportSessionCreate struct {
					Session *struct {
						Metadata struct{ Namespace, Name string }
					}
				}
			}](ctx, `mutation ($input: EnvironmentDatabaseImportSessionCreateInput!) {
  environmentDatabaseImportSessionCreate(input: $input) {
    session {
      metadata {
        namespace
        name
      }
    }
  }
}`, map[string]any{"input": map[string]any{"environmentId": target.ID, "engine": engine, "fileId": fileID, "idempotencyKey": rand.Text()}})
			if err != nil {
				return err
			}
			if session.EnvironmentDatabaseImportSessionCreate.Session == nil {
				return errors.New("The import of the file did not start")
			}
			meta := session.EnvironmentDatabaseImportSessionCreate.Session.Metadata
			sessionID := "gid://deployyy/DatabaseImportSession/" + meta.Namespace + "/" + meta.Name
			if len(values) > 0 {
				if _, err := query[struct{}](ctx, `mutation ($input: EnvironmentDatabaseImportSessionVariablesUpdateInput!) {
  environmentDatabaseImportSessionVariablesUpdate(input: $input) {
    clientMutationId
  }
}`, map[string]any{"input": map[string]any{"sessionId": sessionID, "variables": values}}); err != nil {
					return err
				}
			}

			input := map[string]any{"sessionId": sessionID, "replace": true, "freeze": freeze, "idempotencyKey": rand.Text()}
			if running {
				input["into"] = "RUNNING_DATABASE"
				if name != "" {
					input["database"] = name
				}
				if noBackup {
					input["skipBackup"] = true
				}
			}
			// The file of the session is ready within seconds.
			var created struct {
				EnvironmentDatabaseImportCreate struct {
					Change *struct{ ID string }
				}
			}
			for deadline := time.Now().Add(pollLimit); ; {
				created, err = query[struct {
					EnvironmentDatabaseImportCreate struct {
						Change *struct{ ID string }
					}
				}](ctx, `mutation ($input: EnvironmentDatabaseImportCreateInput!) {
  environmentDatabaseImportCreate(input: $input) {
    change {
      ... on Node {
        id
      }
    }
  }
}`, map[string]any{"input": input})
				if err == nil || !strings.Contains(err.Error(), "Select a stored archive for this environment first") || time.Now().After(deadline) {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(pollEvery):
				}
			}
			if err != nil {
				return err
			}
			if created.EnvironmentDatabaseImportCreate.Change == nil {
				return errors.New("The import did not start")
			}
			return followImport(ctx, target.ID, created.EnvironmentDatabaseImportCreate.Change.ID)
		},
	}
	load.Flags().StringVar(&environment, "environment", "", "the environment as <team>/<project>/<app>/<branch>")
	load.Flags().BoolVar(&running, "into-running", false, "load into the running database server")
	load.Flags().StringVar(&name, "database", "", "the database that a file without database names loads into")
	load.Flags().BoolVar(&noBackup, "no-backup", false, "load without the backup of the environment before the load")
	load.Flags().BoolVar(&freeze, "freeze", false, "stop the processes and show the maintenance page while the data moves")
	load.Flags().StringArrayVarP(&variables, "variable", "v", nil, "a variable of the imported data as NAME=value, such as APP_KEY")
	_ = load.MarkFlagRequired("environment")
	database.AddCommand(load)
	return database
}

// An environment, as an import reads it.
type importTarget struct {
	ID           string
	Level        string
	OwnerProject struct{ ID string }
	Status       struct {
		Services []struct{ Kind, Engine string }
	}
}

// Returns the environment <team>/<project>/<app>/<branch>.
func findEnvironment(ctx context.Context, name string) (importTarget, error) {
	scope := strings.SplitN(name, "/", 4)
	if len(scope) != 4 {
		return importTarget{}, errors.New("Name the environment as <team>/<project>/<app>/<branch>")
	}
	found, err := query[struct {
		Viewer *struct {
			Teams struct {
				Nodes []struct {
					Projects struct {
						Nodes []struct {
							Apps struct {
								Nodes []struct {
									Environments struct{ Nodes []importTarget }
								}
							}
						}
					}
				}
			}
		}
	}](ctx, `query ($team: String!, $project: String!, $app: String!, $branch: String!) {
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
                    ownerProject {
                      id
                    }
                    status {
                      services {
                        kind
                        engine
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
}`, map[string]any{"team": scope[0], "project": scope[1], "app": scope[2], "branch": scope[3]})
	if err != nil {
		return importTarget{}, err
	}
	if found.Viewer == nil {
		return importTarget{}, fmt.Errorf("Not signed in at %s. Run: deployyy login", apiOrigin())
	}
	teams := found.Viewer.Teams.Nodes
	if len(teams) == 0 || len(teams[0].Projects.Nodes) == 0 || len(teams[0].Projects.Nodes[0].Apps.Nodes) == 0 ||
		len(teams[0].Projects.Nodes[0].Apps.Nodes[0].Environments.Nodes) == 0 {
		return importTarget{}, fmt.Errorf("No environment %s", name)
	}
	return teams[0].Projects.Nodes[0].Apps.Nodes[0].Environments.Nodes[0], nil
}

// Returns the ID of the stored file with the path name in the Files area of the project.
func storedFile(ctx context.Context, projectID, name string) (string, error) {
	folder := path.Dir(name)
	if folder == "." {
		folder = ""
	}
	listed, err := query[struct {
		Node *struct {
			Files *struct {
				Nodes []struct {
					ID, FileName string
					Status       *struct{ Phase string }
				}
			}
		}
	}](ctx, `query ($id: ID!, $folder: String) {
  node(id: $id) {
    ... on Project {
      files(folder: $folder, first: 500) {
        nodes {
          id
          fileName
          status {
            phase
          }
        }
      }
    }
  }
}`, map[string]any{"id": projectID, "folder": folder})
	if err != nil {
		return "", err
	}
	if listed.Node != nil && listed.Node.Files != nil {
		for _, file := range listed.Node.Files.Nodes {
			if file.FileName == name && file.Status != nil && file.Status.Phase == "Ready" {
				return file.ID, nil
			}
		}
	}
	return "", fmt.Errorf("%s is no local file and no stored file of the Files area", name)
}

// Prints each new sentence of the import until it ends, and how to approve a step that waits for a person.
func followImport(ctx context.Context, environmentID, changeID string) error {
	said, asked := "", false
	for {
		read, err := query[struct {
			Node *struct {
				Status *struct {
					Phase, Message string
					Noticed        []struct{ Key, Value string }
					Steps          []struct {
						Progress *struct {
							Done, Of int64
							Unit     string
						}
					}
				}
				Actions *struct{ Command struct{ Allowed bool } }
			}
		}](ctx, `query ($id: ID!) {
  node(id: $id) {
    ... on Change {
      status {
        phase
        message
        ... on SetupStatus {
          noticed {
            key
            value
          }
          steps {
            progress {
              done
              of
              unit
            }
          }
        }
      }
    }
    ... on DatabaseImport {
      actions {
        command(command: APPROVE_CUTOVER) {
          allowed
        }
      }
    }
  }
}`, map[string]any{"id": changeID})
		if err != nil {
			return err
		}
		if read.Node == nil || read.Node.Status == nil {
			return errors.New("The import is gone")
		}
		status := read.Node.Status
		line := status.Message
		for _, step := range status.Steps {
			if status.Phase == "RUNNING" && step.Progress != nil && step.Progress.Of > 0 && step.Progress.Unit != "steps" {
				line += fmt.Sprintf(" %d%% of %d %s.", min(step.Progress.Done*100/step.Progress.Of, 100), step.Progress.Of, step.Progress.Unit)
			}
		}
		for _, notice := range status.Noticed {
			if notice.Key == "backup" {
				line += " A restore of backup " + notice.Value + " undoes the import."
			}
		}
		if line != said {
			said = line
			fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
		}
		if read.Node.Actions != nil && read.Node.Actions.Command.Allowed && !asked {
			asked = true
			fmt.Fprintf(os.Stderr, "The import waits for an approval. Approve it in the console, or run:\n\n  deployyy graphql 'mutation ($input: EnvironmentImportCommandInput!) { environmentImportCommand(input: $input) { change { __typename } } }' -v 'input={\"environmentId\":\"%s\",\"changeId\":\"%s\",\"command\":\"APPROVE_CUTOVER\"}'\n\n", environmentID, changeID)
		}
		switch {
		case status.Phase == "SUCCEEDED":
			fmt.Println(status.Message)
			return nil
		case slices.Contains([]string{"FAILED", "CANCELLED", "REFUSED", "STOPPED"}, status.Phase):
			return errors.New(status.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * pollEvery):
		}
	}
}
