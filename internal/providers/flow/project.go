package flow

import (
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

// BuildCreateProjectRequest mirrors the web client's CreateProject call:
// ["projects/*", [null, [title]], [null, 22]]. It carries no reCAPTCHA context.
func BuildCreateProjectRequest(title string) ([]byte, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("thiếu tên project")
	}
	return json.Marshal([]any{"projects/*", []any{nil, []any{title}}, []any{nil, FlowSurfaceID}})
}

// parseCreatedProject reads the project ID from field 1 of the CreateProject reply.
func parseCreatedProject(payload string) (string, error) {
	var fields []any
	if err := json.Unmarshal([]byte(payload), &fields); err != nil || len(fields) == 0 {
		return "", errors.New("phản hồi CreateProject không đúng cấu trúc")
	}
	name, _ := fields[0].(string)
	id := strings.TrimPrefix(name, "projects/")
	if _, err := uuid.Parse(id); err != nil {
		return "", errors.New("phản hồi CreateProject không có project ID")
	}
	return id, nil
}

type flowProject struct {
	ID        string
	Title     string
	UpdatedAt time.Time
}

// FetchAccountProjects queries RPC UpteDb (/Provider.GetProjects) and returns the
// account's projects, most recently updated first.
func (s *Provider) FetchAccountProjects(ctx context.Context, client *chrome.FlowClient) ([]flowProject, error) {
	rpcID := client.Credentials().RPCs.ID(chrome.MethodProjects)
	fReq, err := BuildBatchEnvelope(rpcID, []any{"projects/*", 21, nil, nil, nil, nil, []any{1}})
	if err != nil {
		return nil, err
	}
	limits := s.cfg.Get().FlowPagePolicy()
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.RequestTimeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	payload, err := chrome.ParseRPCPayload(string(body), rpcID)
	if err != nil {
		return nil, err
	}
	return parseProjects(payload)
}

// parseProjects decodes [[[projectID, [title, description, [seconds, nanos], ...]], ...]].
func parseProjects(payload string) ([]flowProject, error) {
	var outer []any
	if err := json.Unmarshal([]byte(payload), &outer); err != nil {
		return nil, errors.New("phản hồi GetProjects không đúng cấu trúc")
	}
	if len(outer) == 0 || outer[0] == nil {
		return nil, nil
	}
	var projects []flowProject
	for _, p := range asList(outer[0]) {
		entry := asList(p)
		if len(entry) < 2 {
			continue
		}
		id, _ := entry[0].(string)
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		project := flowProject{ID: id}
		meta := asList(entry[1])
		if len(meta) > 0 {
			project.Title, _ = meta[0].(string)
		}
		if len(meta) > 2 {
			if stamp := asList(meta[2]); len(stamp) == 2 {
				sec, _ := stamp[0].(float64)
				nanos, _ := stamp[1].(float64)
				project.UpdatedAt = time.Unix(int64(sec), int64(nanos))
			}
		}
		projects = append(projects, project)
	}
	sort.SliceStable(projects, func(i, j int) bool { return projects[i].UpdatedAt.After(projects[j].UpdatedAt) })
	return projects, nil
}

// CreateAccountProject creates a project the way the Flow web client does and
// returns its server-generated ID.
func (s *Provider) CreateAccountProject(ctx context.Context, client *chrome.FlowClient, title string) (string, error) {
	inner, err := BuildCreateProjectRequest(title)
	if err != nil {
		return "", err
	}
	rpcID := client.Credentials().RPCs.ID(chrome.MethodCreateProject)
	fReq, err := BuildBatchEnvelope(rpcID, inner)
	if err != nil {
		return "", err
	}
	limits := s.cfg.Get().FlowPagePolicy()
	body, err := client.Do(ctx, rpcID, fReq, time.Duration(limits.RequestTimeoutSeconds)*time.Second)
	if err != nil {
		return "", err
	}
	payload, err := chrome.ParseRPCPayload(string(body), rpcID)
	if err != nil {
		return "", err
	}
	return parseCreatedProject(payload)
}

// resolveProject returns the requested project or the account's most recently
// updated one, creating a project when the account has none. A missing project
// is an error; it is never sent as null.
func (s *Provider) resolveProject(ctx context.Context, client *chrome.FlowClient, acc session.StoredAccount, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	// Serialize per account so two first requests cannot both create a project.
	lockValue, _ := s.projectResolveLocks.LoadOrStore(acc.ID, &sync.Mutex{})
	resolveLock := lockValue.(*sync.Mutex)
	resolveLock.Lock()
	defer resolveLock.Unlock()
	s.mu.Lock()
	cached := s.projects[acc.ID]
	s.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	projects, err := s.FetchAccountProjects(ctx, client)
	if err != nil {
		return "", fmt.Errorf("không lấy được danh sách project: %w", err)
	}
	var projectID string
	if len(projects) > 0 {
		projectID = projects[0].ID
	} else {
		projectID, err = s.CreateAccountProject(ctx, client, time.Now().Format(projectTitleLayout))
		if err != nil {
			return "", fmt.Errorf("tài khoản chưa có project và không tự tạo được project mới: %w", err)
		}
		logger.Get().Info("created Flow project for account without projects", "account", acc.ID, "project", projectID)
	}
	s.mu.Lock()
	s.projects[acc.ID] = projectID
	s.mu.Unlock()
	return projectID, nil
}

func (s *Provider) forgetProject(accountID string) {
	s.mu.Lock()
	delete(s.projects, accountID)
	s.mu.Unlock()
}
