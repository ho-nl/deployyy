# deployyy CLI

1. Install the CLI in `~/.local/bin`:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/ho-nl/deployyy/main/install.sh | sh
   ```

2. Sign in through the browser:

   ```bash
   deployyy login
   ```

3. Run a query, a mutation or a subscription of the GraphQL API of Deployyy:

   ```bash
   deployyy graphql '{ viewer { teams { nodes { slug } } } }'
   ```

   ```bash
   deployyy graphql 'mutation ($input: EnvironmentRestartInput!) { environmentRestart(input: $input) { changeId } }' -v 'input={"environmentId":"<id>"}'
   ```

   ```bash
   deployyy graphql -v team=acme < projects.graphql
   ```

   ```bash
   deployyy graphql 'subscription ($id: ID!) { nodeUpdates(id: $id) { type } }' -v id=<id>
   ```

   `-v name=value` sets a variable. A JSON value keeps its type, and each other value is a string. The command prints `data` and `errors` of the response and exits with 1 when the response has errors. A subscription prints each event as one line until the API ends the stream or Ctrl-C.

4. Open a shell in an environment, named as `<team>/<project>/<app>/<branch>`:

   ```bash
   deployyy shell acme/shop/magento/main
   ```

   The shell runs in a copy of the environment, with the build, the variables and the shared files of the environment. The CLI asks for a confirmation on a production environment. Each terminal window on your machine has its own terminal in the shell, so a script or a second window does not take over your terminal. `exit` in the last terminal ends the copy at once.

   `--instance <name>` opens the shell on a running instance. `Environment.instances` of the API lists the names.

5. Run commands from a script or an agent. The CLI prints the output of the commands and exits with the status of the last command:

   ```bash
   echo 'bin/magento indexer:status' | deployyy shell acme/shop/magento/main
   ```

   The copy stays for 15 minutes after the script or a terminal disconnects, so the next script starts at once. Each script types in the terminal `script` of the shell: scripts within 5 minutes continue with the same working directory and variables. Ctrl-C stops the running script.

   The CLI refuses a script on a production environment: a shell on production needs a typed confirmation.

6. Copy files to a location of a project, named as `<team>/<project>`. A location is a file store or a media bucket, such as `images` or `media@main`:

   ```bash
   deployyy files ls --project acme/shop
   deployyy files ls images:storage/app --project acme/shop
   deployyy files upload ./beeld images:storage/app/beeld --project acme/shop
   deployyy files download images:storage/app/beeld ./beeld --project acme/shop
   ```

   The CLI starts a transfer, a Change of the project, and copies with [rclone](https://rclone.org) `v1.75.2`, which it installs in its cache after a check of the SHA-256 of the release. rclone copies 16 files at a time and retries a failed file. A second run copies only new and changed files, by size and change time. The CLI ends the transfer after the copy, also after Ctrl-C. The keys of a transfer expire after one day.

   | Option | Use |
   | --- | --- |
   | `--from deploy@shop.example.com:/var/www/storage/app/beeld` | Copies from a server over your own SSH. The CLI places rclone and the keys of the transfer in a temporary folder on the server, runs the copy there, and removes the folder. The server needs no CLI. |
   | `--prepare` | Prints the commands for a server that you reach in another way. The first argument is the path on that server. |
   | `--delete` | Deletes each file at the target that the source lacks. |
   | `--transfers 16` | The files that rclone copies at the same time. |
   | `--checkers 32` | The files that rclone compares at the same time. |

   Upload a file to the Files area of a project:

   ```bash
   deployyy files upload backup.sql.gz --project acme/shop --path imports
   ```

   The CLI uploads the file in parts, 4 parts at a time, and prints the ID of the stored file. The same command after a broken connection sends only the parts that the store does not hold. Without a terminal, the CLI prints one progress line per part, so a log of a background upload stays readable:

   ```bash
   nohup deployyy files upload backup.sql.gz --project acme/shop --path imports > upload.log 2>&1 &
   ```

7. Import a database file into the running database of an environment:

   ```bash
   deployyy database import backup.sql.gz --environment acme/shop/laravel/preview-1 --into-running
   ```

   A local file goes to the folder `imports` of the Files area first. Any other argument names a stored file by its path in the Files area, such as `imports/backup.sql.gz`. The CLI prints each step of the import and exits with 1 when the import fails. An import that waits for an approval prints the command that approves it.

   | Option | Use |
   | --- | --- |
   | `--into-running` | Loads into the running database server. Each database of the file is replaced, and each other database stays. A snapshot of the database disk before the load is the rollback. Without the option, the import fills a new database server and switches to it. |
   | `--database shop_9999` | The database that a file without database names loads into. Without the option, the main database of the app. |
   | `--no-backup` | Loads without the backup before the load. Nothing undoes the import then. |
   | `--freeze` | Stops the processes and shows the maintenance page while the data moves. |
   | `-v APP_KEY=base64:…` | A variable of the imported data, such as the key that encrypts it. |

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

[`cli_test.go`](cli_test.go) runs sign-in, `graphql`, `shell`, `files upload`, `database import` and `update` against a stub of the console, the sign-in service, the terminal service, the object store, the release server and the rclone downloads.

A tag `v<version>` on `main` starts [`cli.yml`](../.github/workflows/cli.yml). The workflow publishes the programs for macOS and Linux with `SHA256SUMS` as a GitHub release. Each GitHub release of this repository is a release of the CLI: [`install.sh`](../install.sh) and `deployyy update` install the newest one.
