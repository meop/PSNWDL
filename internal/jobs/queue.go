package jobs

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	stdsync "sync"
	"sync/atomic"
	"time"

	"PSNWDL/internal/activity"
	"PSNWDL/internal/config"
	"PSNWDL/internal/psn"
)

const (
	defaultParallelDownloads = 5
	verifyPoolSize           = 4
	progressInterval         = 250 * time.Millisecond
	// PS3/PSVita PKGs store a SHA-1 digest in the final 32 bytes; the body
	// hash excludes them. PS4/PS5 hash the whole file.
	ps3TrailerBytes = 32
)

// Queue is the bounded scheduler for download + verify jobs.
type Queue struct {
	mu      stdsync.Mutex
	jobs    map[string]*Job
	cancels map[string]context.CancelFunc
	pauses  map[string]chan struct{}

	downloads *downloadLimiter
	verifySem chan struct{}

	http       *http.Client
	emitter    Emitter
	activity   *activity.Sink
	libraryDir string
	retries    int
	idSeq      atomic.Uint64
}

// NewQueue builds a queue. emitter may be NoopEmitter{} for headless use.
func NewQueue(net config.Network, libraryDir string, emitter Emitter, act *activity.Sink) *Queue {
	parallelDownloads := normalizedParallelDownloads(net.ParallelDownloads)
	retries := normalizedRetries(net.Retries)

	return &Queue{
		jobs:       make(map[string]*Job),
		cancels:    make(map[string]context.CancelFunc),
		pauses:     make(map[string]chan struct{}),
		downloads:  newDownloadLimiter(parallelDownloads),
		verifySem:  make(chan struct{}, verifyPoolSize),
		http:       newHTTPClient(net),
		emitter:    emitter,
		activity:   act,
		libraryDir: libraryDir,
		retries:    retries,
	}
}

func normalizedParallelDownloads(parallelDownloads int) int {
	if parallelDownloads <= 0 {
		return defaultParallelDownloads
	}
	return parallelDownloads
}

func normalizedRetries(retries int) int {
	if retries < 0 {
		return 0
	}
	return retries
}

func newHTTPClient(network config.Network) *http.Client {
	timeout := time.Duration(network.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	// TimeoutSeconds limits connection setup and response headers, not
	// the complete response body. Firmware and game packages can take hours to
	// download; http.Client.Timeout would abort every transfer once this short
	// request timeout elapsed. The request context still provides cancellation.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.ResponseHeaderTimeout = timeout
	transport.TLSHandshakeTimeout = timeout
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: !network.VerifyTLS} // #nosec G402 — PSN endpoints

	return &http.Client{Transport: transport}
}

func (q *Queue) SetLibraryDir(libraryDir string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.libraryDir = libraryDir
}

func (q *Queue) SetNetwork(net config.Network) {
	q.downloads.SetLimit(normalizedParallelDownloads(net.ParallelDownloads))

	q.mu.Lock()
	q.http = newHTTPClient(net)
	q.retries = normalizedRetries(net.Retries)
	q.mu.Unlock()
}

func (q *Queue) httpClient() *http.Client {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.http
}

func (q *Queue) retryLimit() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.retries
}

func (q *Queue) nextID() string {
	return fmt.Sprintf("job-%d", q.idSeq.Add(1))
}

// List returns a snapshot of all known jobs.
func (q *Queue) List() []Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		out = append(out, *j)
	}
	return out
}

// Enqueue registers a job and kicks off its worker goroutine. The caller
// supplies a parent context (typically the app's startup context).
func (q *Queue) Enqueue(parent context.Context, req Request) (string, error) {
	if req.Update.URL == "" {
		return "", errors.New("update has no URL")
	}
	dest, err := q.destinationPath(req)
	if err != nil {
		return "", fmt.Errorf("compute dest path: %w", err)
	}

	kind := req.Kind
	if kind == "" {
		kind = KindTitleUpdate
	}

	j := &Job{
		ID:        q.nextID(),
		TitleID:   req.TitleID,
		TitleName: req.TitleName,
		Mode:      req.Mode,
		Region:    req.Region,
		Kind:      kind,
		Update:    req.Update,
		DestPath:  dest,
		State:     StateQueued,
		Attempt:   1,
	}
	j.MaxAttempts = q.retryLimit() + 1

	q.mu.Lock()
	for _, existing := range q.jobs {
		if existing.DestPath == dest && isActiveJobState(existing.State) {
			id := existing.ID
			q.mu.Unlock()
			return id, nil
		}
	}
	ctx, cancel := context.WithCancel(parent)
	q.jobs[j.ID] = j
	q.cancels[j.ID] = cancel
	q.mu.Unlock()

	q.emitter.Emit(EventJobAdded, *j)
	q.activity.InfoWithJob("jobs", fmt.Sprintf("Enqueued %s %s v%s", req.TitleID, req.TitleName, req.Update.Version), j.ID)

	go q.run(ctx, cancel, j)
	return j.ID, nil
}

func isActiveJobState(state JobState) bool {
	switch state {
	case StateQueued, StateDownloading, StatePaused, StateResuming, StateVerifying:
		return true
	default:
		return false
	}
}

// Cancel stops an in-flight job. No-op for finished jobs.
func (q *Queue) Cancel(id string) error {
	q.mu.Lock()
	cancel, ok := q.cancels[id]
	j := q.jobs[id]
	delete(q.pauses, id)
	q.mu.Unlock()
	if !ok {
		return fmt.Errorf("no active job %s", id)
	}
	cancel()
	if j != nil {
		go func(path string) {
			time.Sleep(100 * time.Millisecond)
			_ = os.Remove(path + ".part")
		}(j.DestPath)
	}
	return nil
}

// Pause suspends a downloading job. No-op for non-downloading jobs.
func (q *Queue) Pause(id string) error {
	q.mu.Lock()
	j, ok := q.jobs[id]
	if !ok {
		q.mu.Unlock()
		return fmt.Errorf("no job %s", id)
	}
	if j.State != StateDownloading {
		q.mu.Unlock()
		return fmt.Errorf("job %s is not downloading (state=%s)", id, j.State)
	}

	if _, exists := q.pauses[id]; exists {
		q.mu.Unlock()
		return fmt.Errorf("job %s is already paused", id)
	}

	newPauseChan := make(chan struct{})
	q.pauses[id] = newPauseChan
	q.mu.Unlock()

	q.setState(j, StatePaused, "")
	return nil
}

// Resume continues a paused job. No-op for non-paused jobs.
func (q *Queue) Resume(id string) error {
	q.mu.Lock()
	pause, ok := q.pauses[id]
	if !ok {
		q.mu.Unlock()
		return fmt.Errorf("job %s is not paused", id)
	}

	j, ok := q.jobs[id]
	if !ok {
		q.mu.Unlock()
		return fmt.Errorf("no job %s", id)
	}

	delete(q.pauses, id)
	close(pause)
	q.mu.Unlock()

	q.setState(j, StateResuming, "")
	go func() {
		q.setState(j, StateDownloading, "")
	}()
	return nil
}

// Retry re-queues a failed or canceled job, clearing any partial download.
// ctx should be the same parent the caller uses for fresh Enqueue calls, so a
// retried job shares the same cancellation lineage as an original one instead
// of running detached from the app's lifecycle.
func (q *Queue) Retry(ctx context.Context, id string) error {
	q.mu.Lock()
	j, ok := q.jobs[id]
	if !ok {
		q.mu.Unlock()
		return fmt.Errorf("no job %s", id)
	}

	if j.State != StateFailed && j.State != StateCanceled {
		state := j.State
		q.mu.Unlock()
		return fmt.Errorf("job %s is not retryable (state=%s)", id, state)
	}
	partPath := j.DestPath + ".part"
	newReq := Request{
		TitleID:   j.TitleID,
		TitleName: j.TitleName,
		Mode:      j.Mode,
		Region:    j.Region,
		Kind:      j.Kind,
		Update:    j.Update,
	}
	q.mu.Unlock()

	if err := os.Remove(partPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove partial file: %w", err)
	}

	_, err := q.Enqueue(ctx, newReq)
	return err
}

func (q *Queue) run(ctx context.Context, cancel context.CancelFunc, j *Job) {
	defer func() {
		cancel()
		q.mu.Lock()
		delete(q.cancels, j.ID)
		q.mu.Unlock()
	}()

	maxAttempts := q.retryLimit() + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	q.mu.Lock()
	j.MaxAttempts = maxAttempts
	q.mu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		q.mu.Lock()
		j.Attempt = attempt
		j.Downloaded = 0
		j.Throughput = 0
		j.ETA = 0
		q.mu.Unlock()

		if attempt > 1 {
			_ = os.Remove(j.DestPath)
			_ = os.Remove(j.DestPath + ".part")
			q.activity.WarnWithJob("jobs", fmt.Sprintf("Retrying %s v%s (%d/%d)", j.TitleID, j.Update.Version, attempt, maxAttempts), j.ID)
		} else {
			q.activity.InfoWithJob("jobs", fmt.Sprintf("Starting download for %s v%s", j.TitleID, j.Update.Version), j.ID)
		}

		// --- Download phase ---
		if !q.downloads.Acquire(ctx) {
			q.setState(j, StateCanceled, "")
			return
		}
		q.setState(j, StateDownloading, "")
		q.activity.InfoWithJob("jobs", fmt.Sprintf("Downloading %s", j.Update.URL), j.ID)
		err := q.download(ctx, j)
		q.downloads.Release()

		if err != nil {
			_ = os.Remove(j.DestPath + ".part")
			if errors.Is(ctx.Err(), context.Canceled) {
				q.setState(j, StateCanceled, "")
				q.activity.WarnWithJob("jobs", "Download canceled", j.ID)
				return
			}
			lastErr = fmt.Errorf("download failed: %w", err)
			q.activity.ErrorWithJob("jobs", fmt.Sprintf("Download failed: %v", err), j.ID)
			if attempt < maxAttempts {
				continue
			}
			break
		}

		q.activity.InfoWithJob("jobs", "Download complete", j.ID)

		// --- Verify phase ---
		select {
		case q.verifySem <- struct{}{}:
		case <-ctx.Done():
			q.setState(j, StateCanceled, "")
			return
		}
		q.setState(j, StateVerifying, "")
		q.activity.InfoWithJob("jobs", "Verifying size and SHA-1", j.ID)
		verr := q.verify(j)
		<-q.verifySem

		if verr != nil {
			lastErr = fmt.Errorf("verify failed: %w", verr)
			q.activity.ErrorWithJob("jobs", fmt.Sprintf("Verify failed: %v", verr), j.ID)
			if attempt < maxAttempts {
				continue
			}
			break
		}

		q.activity.InfoWithJob("jobs", "Verify OK", j.ID)
		lastErr = nil
		break
	}

	if lastErr != nil {
		q.setState(j, StateFailed, lastErr.Error())
		return
	}

	// Download + verify are done. Library installation is a separate explicit
	// application action, never part of the queue.
	q.setState(j, StateDone, "")
	q.activity.InfoWithJob("jobs", "Job complete", j.ID)
}

func (q *Queue) verify(j *Job) error {
	if err := verifySize(j.DestPath, j.Update.Size); err != nil {
		return err
	}

	switch j.Kind {
	case KindFirmware:
		return verifyFull(j.DestPath, j.Update.SHA1Sum)
	default:
		if j.Mode == "ps4" || j.Mode == "ps5" {
			return verifyFull(j.DestPath, j.Update.SHA1Sum)
		}
		return verifyPS3(j.DestPath, j.Update.SHA1Sum)
	}
}

func (q *Queue) setState(j *Job, st JobState, errMsg string) {
	q.mu.Lock()
	j.State = st
	j.Error = errMsg
	snap := *j
	q.mu.Unlock()
	q.emitter.Emit(EventJobState, snap)
}

func (q *Queue) download(ctx context.Context, j *Job) error {
	if err := os.MkdirAll(filepath.Dir(j.DestPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tmpPath := j.DestPath + ".part"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			f.Close()
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.Update.URL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := q.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: status %d", resp.StatusCode)
	}

	pw := &progressWriter{job: j, queue: q, lastEmit: time.Now()}
	pr := &pauseableReader{
		inner: resp.Body,
		queue: q,
		jobID: j.ID,
		ctx:   ctx,
	}
	mw := io.MultiWriter(f, pw)
	if _, err := io.Copy(mw, pr); err != nil {
		return fmt.Errorf("stream body: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	closed = true
	if err := os.Rename(tmpPath, j.DestPath); err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	return nil
}

type downloadLimiter struct {
	mu      stdsync.Mutex
	limit   int
	active  int
	changed chan struct{}
}

func newDownloadLimiter(limit int) *downloadLimiter {
	if limit <= 0 {
		limit = defaultParallelDownloads
	}
	return &downloadLimiter{
		limit:   limit,
		changed: make(chan struct{}),
	}
}

func (l *downloadLimiter) Acquire(ctx context.Context) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		l.mu.Lock()
		if l.active < l.limit {
			l.active++
			l.mu.Unlock()
			return true
		}
		changed := l.changed
		l.mu.Unlock()

		select {
		case <-ctx.Done():
			return false
		case <-changed:
		}
	}
}

func (l *downloadLimiter) Release() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
	}
	l.notifyLocked()
	l.mu.Unlock()
}

func (l *downloadLimiter) SetLimit(limit int) {
	if limit <= 0 {
		limit = defaultParallelDownloads
	}
	l.mu.Lock()
	l.limit = limit
	l.notifyLocked()
	l.mu.Unlock()
}

func (l *downloadLimiter) notifyLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}

type pauseableReader struct {
	inner io.Reader
	queue *Queue
	jobID string
	ctx   context.Context
}

func (r *pauseableReader) Read(p []byte) (n int, err error) {
	for {
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		default:
		}

		r.queue.mu.Lock()
		pauseChan, paused := r.queue.pauses[r.jobID]
		r.queue.mu.Unlock()

		if paused {
			select {
			case <-pauseChan:
			case <-r.ctx.Done():
				return 0, r.ctx.Err()
			}
			continue
		}

		n, err = r.inner.Read(p)
		if n == 0 && err == nil {
			continue
		}
		return n, err
	}
}

type progressWriter struct {
	job      *Job
	queue    *Queue
	lastEmit time.Time
	samples  []samplePoint
}

type samplePoint struct {
	ts    time.Time
	bytes int64
}

const (
	// Throughput is computed over a sliding window of samples. Samples are
	// taken at most every sampleInterval (not on every Write, which at high
	// throughput would fill the window in milliseconds and make the window
	// span far less than throughputWindow). With a 1s interval and a 3s
	// window, MB/s stabilizes within ~3s and stays responsive.
	maxSamples       = 8
	sampleInterval   = time.Second
	throughputWindow = 3 * time.Second
)

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	now := time.Now()

	p.queue.mu.Lock()

	p.job.Downloaded += int64(n)

	// Throttle sampling: only push a new point when enough time has elapsed
	// since the last one. This keeps the window span meaningful.
	if len(p.samples) == 0 || now.Sub(p.samples[len(p.samples)-1].ts) >= sampleInterval {
		p.samples = append(p.samples, samplePoint{ts: now, bytes: p.job.Downloaded})
		if len(p.samples) > maxSamples {
			p.samples = p.samples[len(p.samples)-maxSamples:]
		}
	}

	if len(p.samples) >= 2 {
		oldest := p.samples[0]
		newest := p.samples[len(p.samples)-1]
		window := newest.ts.Sub(oldest.ts)

		if window >= throughputWindow || (len(p.samples) >= 2 && window >= sampleInterval) {
			throughput := float64(newest.bytes-oldest.bytes) / window.Seconds()
			p.job.Throughput = throughput

			if throughput > 0 {
				remaining := float64(p.job.Update.Size - p.job.Downloaded)
				etaSeconds := remaining / throughput
				p.job.ETA = int64(etaSeconds)
			} else {
				p.job.ETA = 0
			}
		}
	}

	shouldEmit := now.Sub(p.lastEmit) >= progressInterval
	if shouldEmit {
		p.lastEmit = now
	}
	snap := *p.job
	p.queue.mu.Unlock()

	if shouldEmit {
		p.queue.emitter.Emit(EventJobProgress, snap)
	}
	return n, nil
}

func (q *Queue) destinationPath(req Request) (string, error) {
	switch req.Mode {
	case "ps3", "ps4", "ps5", "psvita":
	default:
		return "", fmt.Errorf("unsupported mode %q", req.Mode)
	}

	kind := req.Kind
	if kind == "" {
		kind = KindTitleUpdate
	}
	switch kind {
	case KindTitleUpdate, KindTitleUpdateDRMFree:
		if err := psn.ValidateTitleID(req.TitleID); err != nil {
			return "", err
		}
	case KindFirmware:
		if req.TitleID != "firmware" {
			return "", fmt.Errorf("firmware request has invalid title_id %q", req.TitleID)
		}
	default:
		return "", fmt.Errorf("unsupported job kind %q", kind)
	}
	if req.Update.Version == "" {
		return "", errors.New("update missing version")
	}
	parsedURL, err := url.Parse(req.Update.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return "", fmt.Errorf("update has invalid HTTP(S) URL %q", req.Update.URL)
	}
	q.mu.Lock()
	libraryDir := q.libraryDir
	q.mu.Unlock()

	ext := ".pkg"
	var dir string
	if kind == KindFirmware {
		ext = ".pup"
		region := strings.ToLower(strings.TrimSpace(req.Region))
		if region == "" {
			return "", errors.New("firmware request missing region")
		}
		if strings.ContainsAny(region, `\\/:*?"<>|`) || region == "." || region == ".." {
			return "", fmt.Errorf("firmware request has invalid region %q", req.Region)
		}
		firmwareRoot, firmwareErr := config.FirmwareDirForRoot(libraryDir, req.Mode)
		if firmwareErr != nil {
			return "", firmwareErr
		}
		dir = filepath.Join(firmwareRoot, region)
	} else {
		titleRoot, titleErr := config.TitleDirForRoot(libraryDir, req.Mode)
		if titleErr != nil {
			return "", titleErr
		}
		dir = filepath.Join(titleRoot, req.TitleID)
	}
	if err != nil {
		return "", err
	}
	version := sanitizeVersion(req.Update.Version)
	if kind == KindTitleUpdateDRMFree {
		version += "_drm_free"
	}
	filename := fmt.Sprintf("%s_%s%s", req.TitleID, version, ext)
	return filepath.Join(dir, filename), nil
}

func sanitizeVersion(v string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`\\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, v)
}

func verifySize(path string, expected int64) error {
	if expected <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat for size: %w", err)
	}
	if info.Size() != expected {
		return fmt.Errorf("size mismatch: got %d, want %d", info.Size(), expected)
	}
	return nil
}

// verifyFull hashes the entire file and compares to the expected SHA-1.
// Used for firmware PUPs and PS4/PS5 PKGs. Empty expected = skip check.
func verifyFull(path, expectedSHA1 string) error {
	if expectedSHA1 == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open for verify: %w", err)
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expectedSHA1) {
		return fmt.Errorf("sha1 mismatch: got %s, want %s", got, expectedSHA1)
	}
	return nil
}

// verifyPS3 hashes all but the trailing 32 bytes (where Sony stores a digest)
// and compares to the expected SHA-1 from ver.xml. Empty expected = skip check.
func verifyPS3(path, expectedSHA1 string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open for verify: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if info.Size() < ps3TrailerBytes {
		return fmt.Errorf("file too small: %d bytes", info.Size())
	}

	h := sha1.New()
	if _, err := io.CopyN(h, f, info.Size()-ps3TrailerBytes); err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if expectedSHA1 == "" {
		return nil
	}
	if !strings.EqualFold(got, expectedSHA1) {
		return fmt.Errorf("sha1 mismatch: got %s, want %s", got, expectedSHA1)
	}
	return nil
}
