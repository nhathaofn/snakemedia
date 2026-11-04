package tools

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"SnakeMedia/internal/events"
	"SnakeMedia/internal/logger"
)

// Origin is where a program was found.
type Origin string

const (
	OriginNone   Origin = "none"
	OriginConfig Origin = "config"
	OriginSystem Origin = "system"
	OriginApp    Origin = "app"
)

// AllOrigins is bound to the frontend as a TypeScript enum (tools.Origin).
var AllOrigins = []struct {
	Value  Origin
	TSName string
}{
	{OriginNone, "None"},
	{OriginConfig, "Config"},
	{OriginSystem, "System"},
	{OriginApp, "App"},
}

// Program is one executable of a tool and where it was found.
type Program struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Origin Origin `json:"origin"`
}

// Tool is a tool's state for the settings screen and the download prompt.
type Tool struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Homepage string    `json:"homepage"`
	Programs []Program `json:"programs"`
	// Ready is true when every program was found.
	Ready bool `json:"ready"`
	// Downloadable is true when the manifest has a source for this platform.
	Downloadable bool `json:"downloadable"`
	// Version is the app's own copy, empty when there is none.
	Version    string `json:"version"`
	Installing bool   `json:"installing"`
}

// Request is sent (events.EventToolMissing) when a feature needs a tool the
// machine does not have.
type Request struct {
	Tool    Tool   `json:"tool"`
	Feature string `json:"feature"`
}

// Progress is a running download (events.EventToolProgress).
type Progress struct {
	Tool  string `json:"tool"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// Activity is what a screen opened late needs: unanswered asks and running
// downloads.
type Activity struct {
	Requests  []Request  `json:"requests"`
	Downloads []Progress `json:"downloads"`
}

// ErrMissing is returned for a program found nowhere.
var ErrMissing = errors.New("tools: program not found")

// Service finds programs and installs tools into its own folder.
type Service struct {
	manifest  Manifest
	dir       string
	paths     func() map[string]string
	client    *http.Client
	userAgent string

	mu         sync.Mutex
	installing map[string]bool
	// progress is each running download's last progress.
	progress map[string]Progress
	// waiting are the tools features asked for that the user has not
	// answered; postponed ones are not asked for again in this run.
	waiting   map[string]Request
	postponed map[string]bool
	emit      func(events.Event, any)
}

// New makes the service over the embedded manifest. dir is the app's tools
// folder; paths returns the program paths the user set (config tools.paths).
func New(dir, userAgent string, paths func() map[string]string) (*Service, error) {
	m, err := ParseManifest(manifestJSON)
	if err != nil {
		return nil, err
	}
	return newService(m, dir, userAgent, paths), nil
}

func newService(m Manifest, dir, userAgent string, paths func() map[string]string) *Service {
	return &Service{
		manifest: m, dir: dir, paths: paths, client: &http.Client{}, userAgent: userAgent,
		installing: map[string]bool{}, progress: map[string]Progress{}, waiting: map[string]Request{}, postponed: map[string]bool{},
	}
}

// Start lets the service send events.
func (s *Service) Start(emit func(events.Event, any)) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
}

func (s *Service) send(event events.Event, data any) {
	s.mu.Lock()
	emit := s.emit
	s.mu.Unlock()
	if emit != nil {
		emit(event, data)
	}
}

// Program returns the path of program for feature (a short name the user
// knows, e.g. "Xóa watermark video"). When it is found nowhere, the user is
// asked to download its tool (events.EventToolMissing) and ErrMissing is
// returned; nothing is downloaded without the user. A tool the user put off
// (Postpone) is not asked for again in this run.
func (s *Service) Program(feature, program string) (string, error) {
	spec, ok := s.specOf(program)
	if !ok {
		return "", fmt.Errorf("tools: %q is not in the manifest", program)
	}
	p := s.locate(spec, program)
	if p.Origin != OriginNone {
		return p.Path, nil
	}
	req := Request{Tool: s.status(spec), Feature: feature}
	s.mu.Lock()
	ask := !s.postponed[spec.ID] && !s.installing[spec.ID]
	if ask {
		s.waiting[spec.ID] = req
	}
	s.mu.Unlock()
	if ask {
		s.send(events.EventToolMissing, req)
	}
	return "", fmt.Errorf("%w: %s cần %s (%s). Tải trong Cài đặt → Công cụ.", ErrMissing, feature, spec.Name, program)
}

// Requests are the asks the user has not answered, in manifest order, so a
// screen opened after an ask still shows it.
func (s *Service) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Request{}
	for _, spec := range s.manifest.Tools {
		if req, ok := s.waiting[spec.ID]; ok {
			out = append(out, req)
		}
	}
	return out
}

// Postpone answers a tool's ask with "later" for the rest of this run.
func (s *Service) Postpone(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.waiting, id)
	s.postponed[id] = true
}

// Progress is every running download.
func (s *Service) Progress() []Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Progress{}
	for _, spec := range s.manifest.Tools {
		if p, ok := s.progress[spec.ID]; ok {
			out = append(out, p)
		}
	}
	return out
}

// List is every tool's state, in manifest order.
func (s *Service) List() []Tool {
	out := make([]Tool, 0, len(s.manifest.Tools))
	for _, spec := range s.manifest.Tools {
		out = append(out, s.status(spec))
	}
	return out
}

func (s *Service) spec(id string) (Spec, bool) {
	i := slices.IndexFunc(s.manifest.Tools, func(t Spec) bool { return t.ID == id })
	if i < 0 {
		return Spec{}, false
	}
	return s.manifest.Tools[i], true
}

func (s *Service) specOf(program string) (Spec, bool) {
	i := slices.IndexFunc(s.manifest.Tools, func(t Spec) bool { return slices.Contains(t.Programs, program) })
	if i < 0 {
		return Spec{}, false
	}
	return s.manifest.Tools[i], true
}

func (s *Service) status(spec Spec) Tool {
	_, downloadable := spec.source()
	t := Tool{ID: spec.ID, Name: spec.Name, Homepage: spec.Homepage, Ready: true, Downloadable: downloadable}
	for _, name := range spec.Programs {
		p := s.locate(spec, name)
		t.Programs = append(t.Programs, p)
		t.Ready = t.Ready && p.Origin != OriginNone
	}
	if r, err := readReceipt(s.toolDir(spec.ID)); err == nil {
		t.Version = r.Version
	}
	s.mu.Lock()
	t.Installing = s.installing[spec.ID]
	s.mu.Unlock()
	return t
}

// locate finds program in order: the path the user set, the folder of a
// sibling program the user set (a tool ships its programs together), the
// machine (PATH), then the app's tools folder.
func (s *Service) locate(spec Spec, program string) Program {
	paths := map[string]string{}
	if s.paths != nil {
		paths = s.paths()
	}
	if configured := strings.TrimSpace(paths[program]); configured != "" {
		if path, err := exec.LookPath(configured); err == nil {
			return Program{Name: program, Path: path, Origin: OriginConfig}
		}
		logger.Get().Warn("configured tool path not usable", "program", program, "path", configured)
		return Program{Name: program, Origin: OriginNone}
	}
	for _, sibling := range spec.Programs {
		if configured := strings.TrimSpace(paths[sibling]); sibling != program && configured != "" {
			if path, err := exec.LookPath(filepath.Join(filepath.Dir(configured), program)); err == nil {
				return Program{Name: program, Path: path, Origin: OriginConfig}
			}
		}
	}
	if path, err := exec.LookPath(program); err == nil {
		return Program{Name: program, Path: path, Origin: OriginSystem}
	}
	own := filepath.Join(s.toolDir(spec.ID), program+exeSuffix())
	if info, err := os.Stat(own); err == nil && info.Mode().IsRegular() {
		return Program{Name: program, Path: own, Origin: OriginApp}
	}
	return Program{Name: program, Origin: OriginNone}
}

func (s *Service) toolDir(id string) string { return filepath.Join(s.dir, id) }

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// Remove deletes the app's own copy of a tool; copies on the machine stay.
func (s *Service) Remove(id string) error {
	if _, ok := s.spec(id); !ok {
		return fmt.Errorf("Không có công cụ %q.", id)
	}
	s.mu.Lock()
	busy := s.installing[id]
	s.mu.Unlock()
	if busy {
		return errors.New("Công cụ đang được tải.")
	}
	if err := os.RemoveAll(s.toolDir(id)); err != nil {
		return fmt.Errorf("Không xóa được công cụ (có thể đang được dùng): %w", err)
	}
	return nil
}
