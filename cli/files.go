package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The block of the fingerprint: the SHA-256 of the SHA-256 of each block of the file.
const fingerprintBlock = 8 << 20

// Parts in one grant of signed URLs, and parts that upload at the same time.
const (
	grantParts    = 8
	parallelParts = 4
)

// How often the CLI reads the state of an upload, and how long it waits for one state.
var (
	pollEvery = time.Second
	pollLimit = 10 * time.Minute
)

func filesCommand() *cobra.Command {
	files := &cobra.Command{Use: "files", Short: "Files of a project"}
	var folder string
	var upload transferOptions
	uploadCommand := &cobra.Command{
		Use:   "upload <source> [<location>:<path>]",
		Short: "Copy files to a location of a project, or upload a file to the Files area",
		Long: `Copies a local file or folder to a location of a project, such as images:storage/app/beeld. A second run copies only new and changed files.
With --from, the copy runs on a server that you reach over SSH. With --prepare, the CLI prints the commands for a server without the CLI.
With one argument and --path, the CLI uploads one file to the Files area in parts and prints the ID of the stored file.`,
		Example: `  deployyy files upload ./beeld images:storage/app/beeld --project acme/shop
  deployyy files upload --from deploy@shop.example.com:/var/www/storage/app/beeld images:storage/app/beeld --project acme/shop
  deployyy files upload --prepare /var/www/storage/app/beeld images:storage/app/beeld --project acme/shop
  deployyy files upload backup.sql.gz --project acme/shop --path imports`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			ctx := command.Context()
			target, source := args[len(args)-1], ""
			if len(args) == 2 {
				source = args[0]
			}
			location, path, ok := locationPath(target)
			if !ok {
				if len(args) != 1 || upload.from != "" || upload.prepare {
					return errors.New("Name the target as <location>:<path>. deployyy files ls lists the locations")
				}
				projectID, err := projectID(ctx, upload.project)
				if err != nil {
					return err
				}
				id, err := uploadFile(ctx, projectID, args[0], pathpkg.Join(folder, filepath.Base(args[0])))
				if err != nil {
					return err
				}
				fmt.Println(id)
				return nil
			}
			verb := "copy"
			if upload.mirror {
				verb = "sync"
			}
			switch {
			case upload.from != "" && source != "":
				return errors.New("Name the source once: as an argument or with --from")
			case upload.from != "":
				server, remotePath, found := strings.Cut(upload.from, ":")
				if !found || remotePath == "" {
					return errors.New("Name the source as --from <user>@<host>:<path>")
				}
				return runTransfer(ctx, upload.project, location, "WRITE", func(c transferConnection, read func(io.Reader) copyReport) error {
					return runRemote(ctx, server, c, read, append([]string{verb, remotePath, c.remote(path)}, copyFlags(upload.transfers, upload.checkers)...)...)
				})
			case source == "":
				return errors.New("Name the source: a local path, --from <user>@<host>:<path>, or a path with --prepare")
			case upload.prepare:
				projectID, err := projectID(ctx, upload.project)
				if err != nil {
					return err
				}
				_, connection, err := startTransfer(ctx, projectID, location, "WRITE")
				if err != nil {
					return err
				}
				printPrepared(connection, []string{verb, source, connection.remote(path), "--transfers", fmt.Sprint(upload.transfers), "--checkers", fmt.Sprint(upload.checkers),
					"--s3-chunk-size", "16M", "--exclude", "lost+found/**", "--progress"})
				return nil
			}
			return runTransfer(ctx, upload.project, location, "WRITE", func(c transferConnection, read func(io.Reader) copyReport) error {
				return runLocal(ctx, c, read, append([]string{verb, source, c.remote(path)}, copyFlags(upload.transfers, upload.checkers)...)...)
			})
		},
	}
	upload.flags(uploadCommand)
	uploadCommand.Flags().StringVar(&folder, "path", "", "the folder in the Files area")
	uploadCommand.Flags().StringVar(&upload.from, "from", "", "copy from <user>@<host>:<path> over your own SSH")
	uploadCommand.Flags().BoolVar(&upload.prepare, "prepare", false, "print the commands for a server without the CLI")

	var download transferOptions
	downloadCommand := &cobra.Command{
		Use:     "download <location>:<path> <target>",
		Short:   "Copy files from a location of a project",
		Long:    "Copies a folder or a file of a location of a project to a local folder. A second run copies only new and changed files.",
		Example: `  deployyy files download images:storage/app/beeld ./beeld --project acme/shop`,
		Args:    cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			location, path, ok := locationPath(args[0])
			if !ok {
				return errors.New("Name the source as <location>:<path>. deployyy files ls lists the locations")
			}
			verb := "copy"
			if download.mirror {
				verb = "sync"
			}
			ctx := command.Context()
			return runTransfer(ctx, download.project, location, "READ", func(c transferConnection, read func(io.Reader) copyReport) error {
				return runLocal(ctx, c, read, append([]string{verb, c.remote(path), args[1]}, copyFlags(download.transfers, download.checkers)...)...)
			})
		},
	}
	download.flags(downloadCommand)

	var lsProject string
	lsCommand := &cobra.Command{
		Use:   "ls [<location>[:<path>]]",
		Short: "List the locations of a project, or the folders and files of a location",
		Example: `  deployyy files ls --project acme/shop
  deployyy files ls images:storage/app --project acme/shop`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listLocations(command.Context(), lsProject)
			}
			location, path, ok := locationPath(args[0])
			if !ok {
				location, path = args[0], ""
			}
			return listEntries(command.Context(), lsProject, location, path)
		},
	}
	lsCommand.Flags().StringVar(&lsProject, "project", "", "the project as <team>/<project>")
	_ = lsCommand.MarkFlagRequired("project")
	files.AddCommand(lsCommand, uploadCommand, downloadCommand)
	return files
}

// projectQuery runs a query of one project <team>/<project>, with the fields of fields in the project.
func projectQuery[Project any](ctx context.Context, name, variables, fields string, values map[string]any) (Project, error) {
	var zero Project
	scope := strings.SplitN(name, "/", 2)
	if len(scope) != 2 {
		return zero, errors.New("Name the project as <team>/<project>")
	}
	values["team"], values["project"] = scope[0], scope[1]
	found, err := query[struct {
		Viewer *struct {
			Teams struct {
				Nodes []struct{ Projects struct{ Nodes []Project } }
			}
		}
	}](ctx, `query ($team: String!, $project: String!`+variables+`) {
  viewer {
    teams(filter: { slug: $team }) {
      nodes {
        projects(filter: { slug: $project, state: ACTIVE }, first: 1, includeShared: true) {
          nodes {
            `+fields+`
          }
        }
      }
    }
  }
}`, values)
	if err != nil {
		return zero, err
	}
	if found.Viewer == nil {
		return zero, fmt.Errorf("Not signed in at %s. Run: deployyy login", apiOrigin())
	}
	if len(found.Viewer.Teams.Nodes) != 1 || len(found.Viewer.Teams.Nodes[0].Projects.Nodes) != 1 {
		return zero, fmt.Errorf("No project %s", name)
	}
	return found.Viewer.Teams.Nodes[0].Projects.Nodes[0], nil
}

func listLocations(ctx context.Context, project string) error {
	found, err := projectQuery[struct {
		FileLocations []struct {
			Name, Kind  string
			Environment *struct{ Branch string }
			Folders     []string
		}
	}](ctx, project, "", `fileLocations {
              name
              kind
              environment {
                branch
              }
              folders
            }`, map[string]any{})
	if err != nil {
		return err
	}
	if len(found.FileLocations) == 0 {
		fmt.Fprintf(os.Stderr, "The project %s has no locations\n", project)
	}
	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, l := range found.FileLocations {
		place := "project"
		if l.Environment != nil {
			place = l.Environment.Branch
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", l.Name, strings.ToLower(strings.ReplaceAll(l.Kind, "_", " ")), place, strings.Join(l.Folders, ", "))
	}
	return out.Flush()
}

func listEntries(ctx context.Context, project, location, path string) error {
	found, err := projectQuery[struct {
		FileLocations []struct {
			Entries []struct {
				Name       string
				Folder     bool
				Size       *int64
				ModifiedAt *time.Time
			}
		}
	}](ctx, project, ", $location: String!, $path: String", `fileLocations(name: $location) {
              entries(path: $path) {
                name
                folder
                size
                modifiedAt
              }
            }`, map[string]any{"location": location, "path": path})
	if err != nil {
		return err
	}
	if len(found.FileLocations) != 1 {
		return fmt.Errorf("The project %s has no location %s. deployyy files ls lists the locations", project, location)
	}
	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, e := range found.FileLocations[0].Entries {
		size, modified, name := "", "", e.Name
		if e.Folder {
			name += "/"
		}
		if e.Size != nil {
			size = humanSize(*e.Size)
		}
		if e.ModifiedAt != nil {
			modified = e.ModifiedAt.UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(out, "%s\t%s\t%s\n", size, modified, name)
	}
	return out.Flush()
}

// Returns the ID of the project <team>/<project>.
func projectID(ctx context.Context, name string) (string, error) {
	found, err := projectQuery[struct{ ID string }](ctx, name, "", "id", map[string]any{})
	return found.ID, err
}

// The state of an upload, as the API reports it.
type uploadState struct {
	Phase, Summary, RequestID string
	PartCount                 int
	PartSize                  int64
	UploadedParts             []int
}

func readUpload(ctx context.Context, id string) (uploadState, error) {
	read, err := query[struct {
		Node *struct{ Status *uploadState }
	}](ctx, `query ($id: ID!) {
  node(id: $id) {
    ... on ProjectFile {
      status {
        phase
        summary
        partCount
        partSize
        uploadedParts
        requestId
      }
    }
  }
}`, map[string]any{"id": id})
	if err != nil {
		return uploadState{}, err
	}
	if read.Node == nil || read.Node.Status == nil {
		return uploadState{}, errors.New("The file is gone")
	}
	if read.Node.Status.Phase == "Failed" {
		return uploadState{}, fmt.Errorf("The upload failed: %s", read.Node.Status.Summary)
	}
	return *read.Node.Status, nil
}

// Reads the upload until ready accepts the state.
func waitUpload(ctx context.Context, id string, ready func(uploadState) bool) (uploadState, error) {
	for deadline := time.Now().Add(pollLimit); ; {
		state, err := readUpload(ctx, id)
		if err != nil || ready(state) {
			return state, err
		}
		if time.Now().After(deadline) {
			return state, errors.New("The upload did not continue in time. Run the same command again to resume")
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// Prints the progress of an upload on standard error: one line that changes in a terminal, else one line per step, so a log file stays readable.
type progress struct {
	mu       sync.Mutex
	terminal bool
	name     string
	size     int64
	done     int64
}

func (p *progress) say(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	line := fmt.Sprintf(format, args...)
	if p.terminal {
		fmt.Fprintf(os.Stderr, "\r\033[K%s", line)
		return
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
}

func (p *progress) add(part, parts int, bytes int64) {
	p.mu.Lock()
	p.done += bytes
	done := p.done
	p.mu.Unlock()
	p.say("%s: part %d of %d stored, %d%% (%s of %s)", p.name, part, parts, done*100/max(p.size, 1), humanSize(done), humanSize(p.size))
}

func (p *progress) end() {
	if p.terminal {
		fmt.Fprintln(os.Stderr)
	}
}

func humanSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (bytes+1023)/1024)
}

// Returns the fingerprint of the file.
func fingerprint(file *os.File, size int64, p *progress) (string, error) {
	joined := sha256.New()
	block := make([]byte, fingerprintBlock)
	for offset := int64(0); offset < size; offset += fingerprintBlock {
		n, err := file.ReadAt(block, offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		sum := sha256.Sum256(block[:n])
		joined.Write(sum[:])
		if offset/fingerprintBlock%64 == 63 {
			p.say("%s: checked %d%%", p.name, (offset+int64(n))*100/size)
		}
	}
	return hex.EncodeToString(joined.Sum(nil)), nil
}

// Uploads the local file to the Files area of the project with the path name, and returns the ID of the stored file.
func uploadFile(ctx context.Context, projectID, local, name string) (string, error) {
	file, err := os.Open(local)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size == 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is empty or no regular file", local)
	}
	p := &progress{terminal: term.IsTerminal(int(os.Stderr.Fd())), name: name, size: size}
	defer p.end()
	p.say("%s: checking %s", name, humanSize(size))
	sum, err := fingerprint(file, size, p)
	if err != nil {
		return "", err
	}
	started, err := query[struct {
		ProjectFileUploadStart struct {
			File *struct {
				ID     string
				Status *struct{ Phase string }
			}
		}
	}](ctx, `mutation ($input: ProjectFileUploadStartInput!) {
  projectFileUploadStart(input: $input) {
    file {
      id
      status {
        phase
      }
    }
  }
}`, map[string]any{"input": map[string]any{"projectId": projectID, "fileName": name, "size": size, "fingerprint": sum}})
	if err != nil {
		return "", err
	}
	stored := started.ProjectFileUploadStart.File
	if stored == nil {
		return "", errors.New("The Files area refused the upload")
	}
	if stored.Status != nil && stored.Status.Phase == "Ready" {
		p.say("%s is stored", name)
		return stored.ID, nil
	}
	state, err := waitUpload(ctx, stored.ID, func(s uploadState) bool { return s.PartSize > 0 && s.PartCount > 0 })
	if err != nil {
		return "", err
	}
	if int64(state.PartCount) != (size+state.PartSize-1)/state.PartSize {
		return "", errors.New("The parts of the upload do not match the file")
	}
	partBytes := func(part int) int64 { return min(state.PartSize, size-int64(part-1)*state.PartSize) }
	// A part that a PUT stored is stored: the list of the API changes only with the next grant.
	sent := map[int]bool{}
	for _, part := range state.UploadedParts {
		sent[part] = true
		p.done += partBytes(part)
	}
	for {
		var missing []int
		for part := 1; part <= state.PartCount; part++ {
			if !sent[part] {
				missing = append(missing, part)
			}
		}
		if len(missing) == 0 {
			break
		}
		batch := missing[:min(grantParts, len(missing))]
		grant, err := grantURLs(ctx, stored.ID, batch)
		if err != nil {
			return "", err
		}
		// The grant lists the parts that the store holds now: a part of a broken earlier run needs no new PUT.
		fresh, err := readUpload(ctx, stored.ID)
		if err != nil {
			return "", err
		}
		for _, part := range fresh.UploadedParts {
			if !sent[part] && slices.Contains(batch, part) {
				sent[part] = true
				p.add(part, state.PartCount, partBytes(part))
			}
		}
		if err := putParts(ctx, file, grant, state.PartSize, partBytes, func(part int) {
			sent[part] = true
			p.add(part, state.PartCount, partBytes(part))
		}, sent); err != nil {
			return "", err
		}
	}
	p.say("%s: completing the upload", name)
	if _, err := query[struct{}](ctx, `mutation ($input: ProjectFileUploadUpdateInput!) {
  projectFileUploadUpdate(input: $input) {
    file {
      id
    }
  }
}`, map[string]any{"input": map[string]any{"fileId": stored.ID, "settings": map[string]any{"complete": true}}}); err != nil {
		return "", err
	}
	if _, err := waitUpload(ctx, stored.ID, func(s uploadState) bool { return s.Phase == "Ready" }); err != nil {
		return "", err
	}
	p.say("%s is stored", name)
	return stored.ID, nil
}

// Returns the signed URLs of the parts, by part number.
func grantURLs(ctx context.Context, fileID string, parts []int) (map[int]string, error) {
	request := rand.Text()
	if _, err := query[struct{}](ctx, `mutation ($input: ProjectFileUploadUpdateInput!) {
  projectFileUploadUpdate(input: $input) {
    file {
      id
    }
  }
}`, map[string]any{"input": map[string]any{"fileId": fileID, "settings": map[string]any{"requestId": request, "parts": parts}}}); err != nil {
		return nil, err
	}
	if _, err := waitUpload(ctx, fileID, func(s uploadState) bool { return s.RequestID == request }); err != nil {
		return nil, err
	}
	claimed, err := query[struct {
		ProjectFileUploadClaim struct {
			Upload *struct {
				RequestID string
				Parts     []struct {
					PartNumber int
					PutURL     string
				}
			}
		}
	}](ctx, `mutation ($input: ProjectFileUploadClaimInput!) {
  projectFileUploadClaim(input: $input) {
    upload {
      requestId
      parts {
        partNumber
        putUrl
      }
    }
  }
}`, map[string]any{"input": map[string]any{"fileId": fileID, "requestId": request}})
	if err != nil {
		return nil, err
	}
	grant := claimed.ProjectFileUploadClaim.Upload
	if grant == nil || grant.RequestID != request || len(grant.Parts) != len(parts) {
		return nil, errors.New("The grant of the upload parts does not match the request")
	}
	urls := map[int]string{}
	for _, part := range grant.Parts {
		if !slices.Contains(parts, part.PartNumber) {
			return nil, errors.New("The grant of the upload parts does not match the request")
		}
		urls[part.PartNumber] = part.PutURL
	}
	return urls, nil
}

// Sends each granted part that is not sent, parallelParts at a time, with three tries per part.
func putParts(ctx context.Context, file *os.File, urls map[int]string, partSize int64, partBytes func(int) int64, stored func(int), sent map[int]bool) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var mu sync.Mutex
	slots := make(chan struct{}, parallelParts)
	var wg sync.WaitGroup
	for part, address := range urls {
		if sent[part] {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			var err error
			for try := 0; try < 3; try++ {
				if err = putPart(ctx, address, io.NewSectionReader(file, int64(part-1)*partSize, partBytes(part)), partBytes(part)); err == nil || ctx.Err() != nil {
					break
				}
				time.Sleep(time.Duration(try+1) * pollEvery)
			}
			if err != nil {
				cancel(fmt.Errorf("Part %d failed: %w. Run the same command again to resume", part, err))
				return
			}
			mu.Lock()
			stored(part)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return context.Cause(ctx)
}

func putPart(ctx context.Context, address string, body io.Reader, size int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, address, body)
	if err != nil {
		return err
	}
	// A signed part URL takes a body of a known length, never a chunked one.
	request.ContentLength = size
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("the store answered %s %s", response.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}
