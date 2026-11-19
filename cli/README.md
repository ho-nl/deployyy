# deployyy CLI

1. Install the CLI in `~/.local/bin`:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/ho-nl/deployyy/main/install.sh | sh
   ```

2. Sign in through the browser:

   ```bash
   deployyy login
   ```

3. Run a query or a mutation of the GraphQL API of Deployyy:

   ```bash
   deployyy graphql '{ viewer { teams { nodes { slug } } } }'
   ```

   ```bash
   deployyy graphql 'mutation ($input: EnvironmentRestartInput!) { environmentRestart(input: $input) { changeId } }' -v 'input={"environmentId":"<id>"}'
   ```

   ```bash
   deployyy graphql -v team=acme < projects.graphql
   ```

   `-v name=value` sets a variable. A JSON value keeps its type, and each other value is a string. The command prints `data` and `errors` of the response and exits with 1 when the response has errors.

4. Open a shell in an environment, named as `<team>/<project>/<app>/<branch>`:

   ```bash
   deployyy shell acme/shop/magento/main
   ```

   The shell runs in a copy of the environment, with the build, the variables and the shared files of the environment. The CLI asks for a confirmation on a production environment.

   `--instance <name>` opens the shell on a running instance. `Environment.instances` of the API lists the names.

5. Run commands from a script. The shell exits at the end of standard input:

   ```bash
   echo 'bin/magento indexer:status' | deployyy shell acme/shop/magento/main
   ```

## Updates

Each command starts `deployyy update` in the background, at most one time each day. `deployyy update` replaces the program with the newest [release](https://github.com/ho-nl/deployyy/releases) and refuses a download with a wrong SHA-256 checksum.

## Sign-in

`deployyy login` registers the CLI as a client at the sign-in service of Deployyy and saves a refresh token in `~/.config/deployyy/accounts.json`. `deployyy logout` revokes the token and deletes the client.

| Variable         | Use                                                                     |
| ---------------- | ----------------------------------------------------------------------- |
| `DEPLOYYY_TOKEN` | An access token that the CLI sends in place of the saved sign-in.       |
| `DEPLOYYY_API`   | The origin of another console, such as `http://localhost:3000`.         |

## Development

```bash
cd cli
go test ./...
go run . graphql '{ viewer { preferredLoginName } }'
```

[`cli_test.go`](cli_test.go) runs sign-in, `graphql`, `shell` and `update` against a stub of the console, the sign-in service, the terminal service and the release server.

A tag `v<version>` on `main` starts [`cli.yml`](../.github/workflows/cli.yml). The workflow publishes the programs for macOS and Linux with `SHA256SUMS` as a GitHub release. Each GitHub release of this repository is a release of the CLI: [`install.sh`](../install.sh) and `deployyy update` install the newest one.
