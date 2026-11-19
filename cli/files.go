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
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	var project, folder string
	upload := &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload a file to the Files area of a project",
		Long:  "Uploads a file to the Files area of a project in parts. An upload of the same file to the same path resumes the parts that are not stored. The CLI prints the progress on standard error and the ID of the stored file on standard output.",
		Example: `  deployyy files upload backup.sql.gz --project acme/shop --path imports
  nohup deployyy files upload backup.sql.gz --project acme/shop > upload.log 2>&1 &`,
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			ctx := command.Context()
			projectID, err := projectID(ctx, project)
			if err != nil {
				return err
			}
			id, err := uploadFile(ctx, projectID, args[0], path.Join(folder, filepath.Base(args[0])))
			if err != nil {
				return err
			}
			fmt.Println(id)
			return nil
		},
	}
	upload.Flags().StringVar(&project, "project", "", "the project as <team>/<project>")
	upload.Flags().StringVar(&folder, "path", "", "the folder in the Files area")
	_ = upload.MarkFlagRequired("project")
	files.AddCommand(upload)
	return files
}

// Returns the ID of the project <team>/<project>.
func projectID(ctx context.Context, name string) (string, error) {
	scope := strings.SplitN(name, "/", 2)
	if len(scope) != 2 {
		return "", errors.New("Name the project as <team>/<project>")
	}
	found, err := query[struct {
		Viewer *struct {
			Teams struct {
				Nodes []struct {
					Projects struct{ Nodes []struct{ ID string } }
				}
			}
		}
	}](ctx, `query ($team: String!, $project: String!) {
  viewer {
    teams(filter: { slug: $team }) {
      nodes {
        projects(filter: { slug: $project, state: ACTIVE }, first: 1, includeShared: true) {
          nodes {
            id
          }
        }
      }
    }
  }
}`, map[string]any{"team": scope[0], "project": scope[1]})
	if err != nil {
		return "", err
	}
	if found.Viewer == nil {
		return "", fmt.Errorf("Not signed in at %s. Run: deployyy login", apiOrigin())
	}
	if len(found.Viewer.Teams.Nodes) == 0 || len(found.Viewer.Teams.Nodes[0].Projects.Nodes) == 0 {
		return "", fmt.Errorf("No project %s", name)
	}
	return found.Viewer.Teams.Nodes[0].Projects.Nodes[0].ID, nil
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
