package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// rclone copies the files of a transfer. The CLI installs this release in its cache and checks each archive against the SHA-256 of the release.
const rcloneVersion = "v1.75.2"

var (
	rcloneDownloads = "https://downloads.rclone.org"
	rcloneSums      = map[string]string{
		"linux-amd64": "349ac8fba6ff65d6247043f1750cdcb518ec5d500ef91463a10d37c0ccdf3702",
		"linux-arm64": "7e1e8d69654941b7b7df84ee74c5f7fb09cce7d5496947fb7bf16b55ff427d10",
		"osx-amd64":   "574e1f31cf1c464e5fb0325cb66fcbdeaf1efa1802bd9b17f13c256fd74b6561",
		"osx-arm64":   "610ab7d48615135dfc60b763858b11a426a0d3d1160c1bb76fb0ae6011b02edd",
	}
	// How often the CLI sends the progress of a copy, and how long it waits for the receiver of a transfer.
	reportEvery  = 10 * time.Second
	receiverWait = 5 * time.Minute
)

// The name of the rclone remote of a transfer.
const transferRemote = "deployyy"

// rclonePlatform is the name of a release archive for an operating system and a CPU, as Go or `uname -sm` names them.
func rclonePlatform(system, machine string) (string, error) {
	system = map[string]string{"darwin": "osx", "Darwin": "osx", "linux": "linux", "Linux": "linux"}[system]
	machine = map[string]string{"amd64": "amd64", "x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[machine]
	if system == "" || machine == "" {
		return "", errors.New("rclone has no release for this server. Use --prepare and install rclone yourself")
	}
	return system + "-" + machine, nil
}

// rcloneProgram returns the rclone program of platform, from the cache or from a checked download.
func rcloneProgram(ctx context.Context, platform string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	name := "rclone-" + rcloneVersion + "-" + platform
	program := filepath.Join(cache, "deployyy", name, "rclone")
	if _, err := os.Stat(program); err == nil {
		return program, nil
	}
	archive, err := download(ctx, rcloneDownloads+"/"+rcloneVersion+"/"+name+".zip")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != rcloneSums[platform] {
		return "", fmt.Errorf("%s.zip has a wrong SHA-256 checksum", name)
	}
	files, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(files.File, func(f *zip.File) bool { return f.Name == name+"/rclone" })
	if i < 0 {
		return "", fmt.Errorf("%s.zip holds no rclone", name)
	}
	reader, err := files.File[i].Open()
	if err != nil {
		return "", err
	}
	defer reader.Close()
	if err := os.MkdirAll(filepath.Dir(program), 0o755); err != nil {
		return "", err
	}
	partial := program + ".partial"
	out, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, reader); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return program, os.Rename(partial, program)
}

// locationPath splits `<location>:<path>`. ok is false for a local path.
func locationPath(argument string) (location, folder string, ok bool) {
	location, folder, ok = strings.Cut(argument, ":")
	if !ok || location == "" || strings.Contains(location, "/") {
		return "", "", false
	}
	return location, strings.Trim(folder, "/"), true
}

// The endpoint and the keys of a running transfer.
type transferConnection struct {
	Endpoint, Bucket, AccessKeyID, SecretAccessKey string
	ExpiresAt                                      time.Time
}

func (c transferConnection) env() []string {
	prefix := "RCLONE_CONFIG_" + strings.ToUpper(transferRemote) + "_"
	return []string{prefix + "TYPE=s3", prefix + "PROVIDER=Rclone", prefix + "ENDPOINT=" + c.Endpoint,
		prefix + "ACCESS_KEY_ID=" + c.AccessKeyID, prefix + "SECRET_ACCESS_KEY=" + c.SecretAccessKey, prefix + "NO_CHECK_BUCKET=true"}
}

func (c transferConnection) config() string {
	return "[" + transferRemote + "]\ntype = s3\nprovider = Rclone\nendpoint = " + c.Endpoint + "\naccess_key_id = " + c.AccessKeyID +
		"\nsecret_access_key = " + c.SecretAccessKey + "\nno_check_bucket = true\n"
}

// remote is the path of folder of the location in the remote of the transfer.
func (c transferConnection) remote(folder string) string {
	return transferRemote + ":" + path.Join(c.Bucket, folder)
}

const transferFields = `transfer {
      id
      status {
        phase
        message
      }
    }
    connection {
      endpoint
      bucket
      accessKeyId
      secretAccessKey
      expiresAt
    }`

type transferPayload struct {
	Transfer struct {
		ID     string
		Status *struct{ Phase, Message string }
	}
	Connection *transferConnection
}

// startTransfer starts a transfer of location with access READ or WRITE, and waits until its receiver runs.
func startTransfer(ctx context.Context, projectID, location, access string) (string, transferConnection, error) {
	started, err := query[struct{ FileTransferStart transferPayload }](ctx, `mutation ($input: FileTransferStartInput!) {
  fileTransferStart(input: $input) {
    `+transferFields+`
  }
}`, map[string]any{"input": map[string]any{"projectId": projectID, "location": location, "access": access}})
	if err != nil {
		return "", transferConnection{}, err
	}
	id := started.FileTransferStart.Transfer.ID
	fmt.Fprintf(os.Stderr, "Starting the transfer of %s\n", location)
	for deadline := time.Now().Add(receiverWait); ; {
		connected, err := query[struct{ FileTransferConnect transferPayload }](ctx, `mutation ($input: FileTransferInput!) {
  fileTransferConnect(input: $input) {
    `+transferFields+`
  }
}`, map[string]any{"input": map[string]any{"fileTransferId": id}})
		if err != nil {
			return id, transferConnection{}, err
		}
		payload := connected.FileTransferConnect
		if payload.Connection != nil {
			return id, *payload.Connection, nil
		}
		if status := payload.Transfer.Status; status != nil && slices.Contains([]string{"FAILED", "CANCELLED", "SUCCEEDED"}, status.Phase) {
			return id, transferConnection{}, errors.New(status.Message)
		}
		if time.Now().After(deadline) {
			return id, transferConnection{}, fmt.Errorf("The transfer of %s did not start in time", location)
		}
		select {
		case <-ctx.Done():
			return id, transferConnection{}, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// The progress of a copy, as rclone counts it.
type copyReport struct {
	Bytes, TotalBytes, Files, TotalFiles, FailedFiles int64
	ExpectedEnd                                       *time.Time
	Failed                                            []string
	speed                                             float64
}

func (r copyReport) input() map[string]any {
	input := map[string]any{"bytes": r.Bytes, "totalBytes": r.TotalBytes, "files": r.Files, "totalFiles": r.TotalFiles,
		"failedFiles": r.FailedFiles, "failed": append([]string{}, r.Failed[:min(len(r.Failed), 50)]...)}
	if r.ExpectedEnd != nil {
		input["expectedEnd"] = r.ExpectedEnd.UTC().Format(time.RFC3339)
	}
	return input
}

// updateTransfer sends the report, and ends the transfer with end.
func updateTransfer(ctx context.Context, id string, report copyReport, end bool) error {
	_, err := query[struct{}](ctx, `mutation ($input: FileTransferUpdateInput!) {
  fileTransferUpdate(input: $input) {
    transfer {
      id
    }
  }
}`, map[string]any{"input": map[string]any{"fileTransferId": id, "report": report.input(), "end": end}})
	return err
}

// readCopy reads the JSON log of rclone: it keeps the newest stats and each file that failed, and passes each other line to standard error.
func readCopy(log io.Reader, location string, update func(copyReport)) copyReport {
	var report copyReport
	terminal := term.IsTerminal(int(os.Stderr.Fd()))
	lines := bufio.NewScanner(log)
	lines.Buffer(nil, 1<<20)
	for lines.Scan() {
		var entry struct {
			Level  string
			Msg    string
			Object string
			Stats  *struct {
				Bytes, TotalBytes, Transfers, TotalTransfers, Errors int64
				Speed                                                float64
				Eta                                                  *float64
			}
		}
		if json.Unmarshal(lines.Bytes(), &entry) != nil {
			fmt.Fprintln(os.Stderr, lines.Text())
			continue
		}
		switch {
		case entry.Stats != nil:
			s := entry.Stats
			report.Bytes, report.TotalBytes, report.Files, report.TotalFiles, report.speed = s.Bytes, s.TotalBytes, s.Transfers, s.TotalTransfers, s.Speed
			report.ExpectedEnd = nil
			if s.Eta != nil {
				end := time.Now().Add(time.Duration(*s.Eta) * time.Second)
				report.ExpectedEnd = &end
			}
			line := fmt.Sprintf("%s: %s of %s, %d of %d files, %s/s", location, humanSize(s.Bytes), humanSize(s.TotalBytes), s.Transfers, s.TotalTransfers, humanSize(int64(s.Speed)))
			if s.Eta != nil {
				line += ", " + (time.Duration(*s.Eta) * time.Second).String() + " left"
			}
			if terminal {
				fmt.Fprintf(os.Stderr, "\r\033[K%s", line)
			} else {
				fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
			}
			update(report)
		case entry.Level == "error" && entry.Object != "":
			if !slices.Contains(report.Failed, entry.Object) {
				report.FailedFiles++
				report.Failed = append(report.Failed, entry.Object)
			}
			fmt.Fprintf(os.Stderr, "\n%s: %s\n", entry.Object, entry.Msg)
		case entry.Level == "error" || entry.Level == "critical":
			fmt.Fprintf(os.Stderr, "\n%s\n", entry.Msg)
		}
	}
	if terminal {
		fmt.Fprintln(os.Stderr)
	}
	return report
}

// copyFlags are the flags of each rclone copy of a transfer. The receiver holds the parts in flight of each file in memory: 16 files of 4 parts of 16 MB fit in its 2 GB.
func copyFlags(transfers, checkers int) []string {
	return []string{"--transfers", fmt.Sprint(transfers), "--checkers", fmt.Sprint(checkers),
		"--s3-chunk-size", "16M", "--s3-upload-concurrency", "4", "--retries", "5", "--low-level-retries", "20",
		"--exclude", "lost+found/**", "--use-json-log", "--stats", "5s", "--stats-log-level", "NOTICE", "-v"}
}

// The options of a transfer command.
type transferOptions struct {
	project, from       string
	mirror, prepare     bool
	transfers, checkers int
}

func (o *transferOptions) flags(command *cobra.Command) {
	command.Flags().StringVar(&o.project, "project", "", "the project as <team>/<project>")
	command.Flags().BoolVar(&o.mirror, "delete", false, "delete each file at the target that the source lacks")
	command.Flags().IntVar(&o.transfers, "transfers", 16, "the files that rclone copies at the same time")
	command.Flags().IntVar(&o.checkers, "checkers", 32, "the files that rclone compares at the same time")
	_ = command.MarkFlagRequired("project")
}

// runTransfer starts a transfer of location, runs the copy of run on its connection, sends the progress and ends the transfer.
func runTransfer(ctx context.Context, project, location, access string, run func(transferConnection, func(io.Reader) copyReport) error) error {
	projectID, err := projectID(ctx, project)
	if err != nil {
		return err
	}
	id, connection, err := startTransfer(ctx, projectID, location, access)
	ended := id == ""
	// The transfer ends also after Ctrl-C or a failed start, so the receiver goes at once.
	defer func() {
		if !ended {
			if end := updateTransfer(context.WithoutCancel(ctx), id, copyReport{}, true); end != nil {
				fmt.Fprintln(os.Stderr, end)
			}
		}
	}()
	if err != nil {
		return err
	}
	var mu sync.Mutex
	var last time.Time
	var final copyReport
	read := func(log io.Reader) copyReport {
		final = readCopy(log, location, func(report copyReport) {
			mu.Lock()
			defer mu.Unlock()
			if time.Since(last) < reportEvery {
				return
			}
			last = time.Now()
			if err := updateTransfer(ctx, id, report, false); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		})
		return final
	}
	copyErr := run(connection, read)
	ended = true
	if err := updateTransfer(context.WithoutCancel(ctx), id, final, true); err != nil {
		return err
	}
	switch {
	case final.FailedFiles > 0:
		return fmt.Errorf("%s failed. Run the same command again to copy them", countFiles(final.FailedFiles))
	case copyErr != nil:
		return fmt.Errorf("The copy failed: %w. Run the same command again to continue", copyErr)
	}
	fmt.Fprintf(os.Stderr, "%s: %s copied (%s)\n", location, countFiles(final.Files), humanSize(final.Bytes))
	return nil
}

// runLocal runs the local rclone with arguments and the keys of connection.
func runLocal(ctx context.Context, connection transferConnection, read func(io.Reader) copyReport, arguments ...string) error {
	program, err := rcloneProgram(ctx, rcloneLocalPlatform())
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, program, arguments...)
	command.Env = append(os.Environ(), connection.env()...)
	command.Env = append(command.Env, "RCLONE_CONFIG="+os.DevNull)
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	read(stderr)
	return command.Wait()
}

func rcloneLocalPlatform() string {
	platform, _ := rclonePlatform(runtime.GOOS, runtime.GOARCH)
	return platform
}

// sshOptions share one login of the person between the calls of a transfer.
func sshOptions(control string) []string {
	return []string{"-o", "ControlMaster=auto", "-o", "ControlPath=" + control, "-o", "ControlPersist=60"}
}

// runRemote places rclone and the keys of connection in a temporary folder on the server of the person, runs the copy there and removes the folder.
func runRemote(ctx context.Context, server string, connection transferConnection, read func(io.Reader) copyReport, arguments ...string) error {
	control, err := os.MkdirTemp("", "deployyy-ssh-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(control)
	options := sshOptions(filepath.Join(control, "%C"))
	ssh := func(stdin io.Reader, script string) (string, error) {
		command := exec.CommandContext(ctx, "ssh", append(append(slices.Clone(options), server), "sh -c "+shellQuote(script))...)
		command.Stdin, command.Stderr = stdin, os.Stderr
		output, err := command.Output()
		return strings.TrimSpace(string(output)), err
	}
	system, err := ssh(nil, "uname -sm")
	if err != nil {
		return fmt.Errorf("ssh %s failed: %w", server, err)
	}
	kernel, machine, _ := strings.Cut(system, " ")
	platform, err := rclonePlatform(kernel, machine)
	if err != nil {
		return err
	}
	program, err := rcloneProgram(ctx, platform)
	if err != nil {
		return err
	}
	binary, err := os.Open(program)
	if err != nil {
		return err
	}
	defer binary.Close()
	fmt.Fprintf(os.Stderr, "Placing rclone on %s\n", server)
	folder, err := ssh(binary, `umask 077 && d=$(mktemp -d "${TMPDIR:-/tmp}/deployyy.XXXXXX") && cat > "$d/rclone" && chmod 700 "$d/rclone" && echo "$d"`)
	if err != nil || !strings.HasPrefix(folder, "/") {
		return fmt.Errorf("placing rclone on %s failed: %v", server, err)
	}
	quoted := make([]string, len(arguments))
	for i, a := range arguments {
		quoted[i] = shellQuote(a)
	}
	script := "set -e\nd=" + shellQuote(folder) + "\ntrap 'rm -rf \"$d\"' EXIT HUP INT TERM\numask 077\ncat > \"$d/rclone.conf\" <<'DEPLOYYY'\n" +
		connection.config() + "DEPLOYYY\n\"$d/rclone\" --config \"$d/rclone.conf\" " + strings.Join(quoted, " ") + "\n"
	command := exec.CommandContext(ctx, "ssh", append(append(slices.Clone(options), server), "sh -s")...)
	command.Stdin = strings.NewReader(script)
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	read(stderr)
	return command.Wait()
}

func countFiles(n int64) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'" }

// printPrepared prints the commands that copy with rclone on a server that the person reaches in another way.
func printPrepared(connection transferConnection, arguments []string) {
	quoted := make([]string, len(arguments))
	for i, a := range arguments {
		quoted[i] = shellQuote(a)
	}
	fmt.Printf(`# Run on the server. The keys expire at %s.
P=linux-amd64  # linux-arm64 on an ARM server
curl -fsSLO %s/%s/rclone-%s-$P.zip
unzip -o -j rclone-%s-$P.zip 'rclone-%s-'$P'/rclone' -d .
`, connection.ExpiresAt.UTC().Format(time.RFC3339), rcloneDownloads, rcloneVersion, rcloneVersion, rcloneVersion, rcloneVersion)
	for _, line := range connection.env() {
		name, value, _ := strings.Cut(line, "=")
		fmt.Printf("export %s=%s\n", name, shellQuote(value))
	}
	fmt.Printf("./rclone %s\n", strings.Join(quoted, " "))
	fmt.Printf("# SHA-256 of each archive: linux-amd64 %s, linux-arm64 %s\n", rcloneSums["linux-amd64"], rcloneSums["linux-arm64"])
}
