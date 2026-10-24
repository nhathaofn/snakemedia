package flow

import (
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/engine"
	"SnakeMedia/internal/providers/flow/session"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"SnakeMedia/internal/models"
	"SnakeMedia/internal/providers/flow/chrome"
	"github.com/google/uuid"
)

func validGenerateRequest() GenerateImageRequest {
	return GenerateImageRequest{Prompt: "a red fox", ModelID: "BELUGA", AspectRatio: "16:9", Count: 2}
}

func TestFlowServiceRotatesOnlyVerifiedAccounts(t *testing.T) {
	accounts := testAccountService(t)
	first := seedFlowAccount(t, accounts, "first@example.invalid", true)
	second := seedFlowAccount(t, accounts, "second@example.invalid", true)
	seedFlowAccount(t, accounts, "unverified@example.invalid", false)
	service := testFlowService(accounts)

	if res := service.GetActiveRotationCount(context.Background()); !res.Success || res.Data != 2 {
		t.Fatalf("expected 2 accounts in rotation, got %+v", res)
	}
	var order []string
	for i := 0; i < 4; i++ {
		acc, err := service.NextAccount()
		if err != nil {
			t.Fatal(err)
		}
		order = append(order, acc.ID)
	}
	want := []string{first.ID, second.ID, first.ID, second.ID}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("rotation order %v, want %v", order, want)
	}
}

func TestFlowServiceRequiresAccounts(t *testing.T) {
	service := testFlowService(testAccountService(t))
	if res := service.GetImageModels(context.Background()); res.Success || res.Code != models.CodeUnavailable {
		t.Fatalf("models without accounts: %+v", res)
	}
	if res := service.GenerateImages(context.Background(), validGenerateRequest()); res.Success || res.Code != models.CodeUnavailable {
		t.Fatalf("generation without accounts: %+v", res)
	}
}

func TestGetImageModelsCachesPerAccountWithoutTakingATurn(t *testing.T) {
	accounts := testAccountService(t)
	first := seedFlowAccount(t, accounts, "first@example.invalid", true)
	seedFlowAccount(t, accounts, "second@example.invalid", true)
	service := testFlowService(accounts)
	calls := 0
	service.fetchLiveModels = func(_ context.Context, acc session.StoredAccount) ([]FlowModel, error) {
		calls++
		if acc.ID != first.ID {
			t.Errorf("models fetched for %s, want the account whose turn is next", acc.ID)
		}
		return []FlowModel{{ID: "BELUGA", Name: "Nano Banana 2.1", Default: true}}, nil
	}

	for i := 0; i < 2; i++ {
		res := service.GetImageModels(context.Background())
		if !res.Success || len(res.Data) != 1 || res.Data[0].ID != "BELUGA" {
			t.Fatalf("unexpected models: %+v", res)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one live fetch within the cache window, got %d", calls)
	}
	if next, err := service.NextAccount(); err != nil || next.ID != first.ID {
		t.Fatalf("reading models consumed a rotation turn: %v %v", next, err)
	}
}

func TestGetImageModelsReportsFetchFailure(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchLiveModels = func(context.Context, session.StoredAccount) ([]FlowModel, error) {
		return nil, errors.New("HTrJv RPC returned an error")
	}
	res := service.GetImageModels(context.Background())
	if res.Success || res.Code != models.CodeUnavailable || !strings.Contains(res.Error, "HTrJv") {
		t.Fatalf("fetch failure hidden: %+v", res)
	}
}

func TestGenerateImagesRejectsIncompleteRequests(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.generateLiveImages = func(context.Context, session.StoredAccount, GenerateImageRequest) ([]GeneratedImageItem, error) {
		t.Fatal("invalid request reached Google Flow")
		return nil, nil
	}
	cases := map[string]func(*GenerateImageRequest){
		"empty prompt":    func(r *GenerateImageRequest) { r.Prompt = "  " },
		"no model":        func(r *GenerateImageRequest) { r.ModelID = "" },
		"unknown ratio":   func(r *GenerateImageRequest) { r.AspectRatio = "2:1" },
		"missing ratio":   func(r *GenerateImageRequest) { r.AspectRatio = "" },
		"invalid quality": func(r *GenerateImageRequest) { r.Quality = "8K" },
		"zero count":      func(r *GenerateImageRequest) { r.Count = 0 },
		"too many":        func(r *GenerateImageRequest) { r.Count = MaxImagesPerRequest + 1 },
		"invalid project": func(r *GenerateImageRequest) { r.ProjectID = "default" },
		"reference not img": func(r *GenerateImageRequest) {
			r.References = []ReferenceImage{{Base64: "AAAA", MimeType: "text/plain", FileName: "a.txt"}}
		},
		"reference no name": func(r *GenerateImageRequest) {
			r.References = []ReferenceImage{{Base64: "AAAA", MimeType: "image/png"}}
		},
		"reference empty": func(r *GenerateImageRequest) {
			r.References = []ReferenceImage{{MimeType: "image/png", FileName: "a.png"}}
		},
		"reference too big": func(r *GenerateImageRequest) {
			r.References = []ReferenceImage{{Base64: strings.Repeat("A", MaxReferenceImageBytes/3*4+8), MimeType: "image/png", FileName: "a.png"}}
		},
	}
	for name, mutate := range cases {
		req := validGenerateRequest()
		mutate(&req)
		if res := service.GenerateImages(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument {
			t.Errorf("%s: expected CodeInvalidArgument, got %+v", name, res)
		}
	}
}

func TestGenerateImagesEnforcesModelReferenceLimit(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchLiveModels = func(context.Context, session.StoredAccount) ([]FlowModel, error) {
		return []FlowModel{
			{ID: "BELUGA", Name: "Nano Banana 2.1", MaxReferenceImages: 2},
			{ID: "TEXT_ONLY", Name: "Text only"},
		}, nil
	}
	dispatched := 0
	service.generateLiveImages = func(_ context.Context, _ session.StoredAccount, req GenerateImageRequest) ([]GeneratedImageItem, error) {
		dispatched++
		if len(req.References) != 2 {
			t.Errorf("references altered before dispatch: %d", len(req.References))
		}
		return []GeneratedImageItem{{ID: "img"}}, nil
	}
	ref := ReferenceImage{Base64: "AAAA", MimeType: "image/png", FileName: "a.png"}

	req := validGenerateRequest()
	req.References = []ReferenceImage{ref, ref, ref}
	if res := service.GenerateImages(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument || !strings.Contains(res.Error, "tối đa 2") {
		t.Fatalf("over-limit references accepted: %+v", res)
	}
	req.ModelID = "TEXT_ONLY"
	req.References = []ReferenceImage{ref}
	if res := service.GenerateImages(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("references sent to a model without reference support: %+v", res)
	}
	req.ModelID = "UNKNOWN"
	if res := service.GenerateImages(context.Background(), req); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("unknown model accepted with references: %+v", res)
	}
	req.ModelID = "BELUGA"
	req.References = []ReferenceImage{ref, ref}
	if res := service.GenerateImages(context.Background(), req); !res.Success || dispatched != 1 {
		t.Fatalf("references within the limit rejected: %+v", res)
	}
}

func TestGenerateImagesHoldsProfileAndForgetsProjectOnFailure(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchLiveModels = func(context.Context, session.StoredAccount) ([]FlowModel, error) {
		return []FlowModel{{ID: "BELUGA", Name: "Nano Banana 2.1"}}, nil
	}
	profile, err := accounts.profilePath(acc.ProfileDir)
	if err != nil {
		t.Fatal(err)
	}

	service.generateLiveImages = func(_ context.Context, got session.StoredAccount, req GenerateImageRequest) ([]GeneratedImageItem, error) {
		accounts.mu.Lock()
		held := accounts.inUse[profile]
		accounts.mu.Unlock()
		if held != 1 {
			t.Errorf("profile not held during generation: %d", held)
		}
		if req.ModelID != "BELUGA" || req.Count != 2 || got.ID != acc.ID {
			t.Errorf("request altered before dispatch: %+v", req)
		}
		return []GeneratedImageItem{{ID: "img-1"}, {ID: "img-2"}}, nil
	}
	if res := service.GenerateImages(context.Background(), validGenerateRequest()); !res.Success || len(res.Data) != 2 {
		t.Fatalf("generation failed: %+v", res)
	}

	service.projects[acc.ID] = uuid.NewString()
	service.generateLiveImages = func(context.Context, session.StoredAccount, GenerateImageRequest) ([]GeneratedImageItem, error) {
		return nil, errors.New("ogiZ0b RPC returned an error")
	}
	res := service.GenerateImages(context.Background(), validGenerateRequest())
	if res.Success || res.Code != models.CodeUnavailable || !strings.Contains(res.Error, acc.Email) {
		t.Fatalf("failure not reported with its account: %+v", res)
	}
	if _, ok := service.projects[acc.ID]; ok {
		t.Fatal("project kept after a failed generation")
	}
	accounts.mu.Lock()
	defer accounts.mu.Unlock()
	if len(accounts.inUse) != 0 {
		t.Fatalf("profile still held after generation: %v", accounts.inUse)
	}
}

type flowRoundTrip func(*http.Request) (*http.Response, error)

func (f flowRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func flowReply(r *http.Request, rpcID, payload string) *http.Response {
	envelope := `)]}'` + "\n\n1\n" + `[["wrb.fr","` + rpcID + `",` + payload + `,null,null,null,"generic"]]`
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(envelope)), Request: r}
}

func TestResolveProjectCreatesOneWhenAccountHasNone(t *testing.T) {
	accounts := testAccountService(t)
	acc := seedFlowAccount(t, accounts, "first@example.invalid", true)
	stored, err := accounts.findAccount(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	created := "5b33e82e-56f4-45d3-9930-ff9da8415981"
	var createBody string
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = flowRoundTrip(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		switch rpc := r.URL.Query().Get("rpcids"); rpc {
		case RPCGetProjects:
			return flowReply(r, rpc, `"[]"`), nil
		case RPCCreateProject:
			createBody = form.Get("f.req")
			return flowReply(r, rpc, `"[\"`+created+`\",[\"Oct 23 - 16:00\"]]"`), nil
		}
		t.Errorf("unexpected RPC %s", r.URL.Query().Get("rpcids"))
		return nil, errors.New("unexpected RPC")
	})

	service := testFlowService(accounts)
	client, err := chrome.NewFlowClient(stored.Credentials, config.DefaultAccountConfig())
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := service.resolveProject(context.Background(), client, stored, "")
	if err != nil {
		t.Fatal(err)
	}
	if projectID != created || service.projects[acc.ID] != created {
		t.Fatalf("created project not used: %q", projectID)
	}
	if !strings.Contains(createBody, `\"projects/*\",[null,[\"`) || !strings.Contains(createBody, `[null,22]`) {
		t.Fatalf("CreateProject request differs from the web client: %s", createBody)
	}

	if _, err := parseCreatedProject(`[null]`); err == nil {
		t.Fatal("a reply without a project ID must be an error")
	}
}

func TestGenerationSkipsSignedOutAccounts(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	seedFlowAccount(t, accounts, "second@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchLiveModels = func(context.Context, session.StoredAccount) ([]FlowModel, error) {
		return []FlowModel{{ID: "BELUGA", Name: "Nano Banana 2.1"}}, nil
	}
	signedOut := map[string]bool{"first@example.invalid": true}
	service.syncSession = func(_ context.Context, acc session.StoredAccount) (session.StoredAccount, error) {
		if signedOut[acc.Email] {
			return acc, session.ErrSessionSignedOut
		}
		return acc, nil
	}
	var used string
	service.generateLiveImages = func(_ context.Context, acc session.StoredAccount, _ GenerateImageRequest) ([]GeneratedImageItem, error) {
		used = acc.Email
		return []GeneratedImageItem{{ID: "img"}}, nil
	}
	if res := service.GenerateImages(context.Background(), validGenerateRequest()); !res.Success || used != "second@example.invalid" {
		t.Fatalf("did not fall over to the signed-in account: %+v used %q", res, used)
	}

	signedOut["second@example.invalid"] = true
	res := service.GenerateImages(context.Background(), validGenerateRequest())
	if res.Success || res.Code != models.CodeUnavailable || !strings.Contains(res.Error, "first@example.invalid") {
		t.Fatalf("all accounts signed out must be reported with their emails: %+v", res)
	}
}

func TestModelsRetryOnceAfterStaleSession(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchModelsPayload = service.fetchModelsPayloadFromAccount
	syncs := 0
	service.syncSession = func(_ context.Context, acc session.StoredAccount) (session.StoredAccount, error) {
		syncs++
		return acc, nil
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = flowRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return flowReply(r, RPCGetModels, `"`+strings.ReplaceAll(getModelsPayload, `"`, `\"`)+`"`), nil
	})
	res := service.GetImageModels(context.Background())
	if !res.Success || len(res.Data) == 0 || syncs != 1 || calls != 2 {
		t.Fatalf("stale session not refreshed once: %+v syncs=%d calls=%d", res, syncs, calls)
	}
}

func TestNodesRunTheProviderCapabilities(t *testing.T) {
	accounts := testAccountService(t)
	seedFlowAccount(t, accounts, "first@example.invalid", true)
	service := testFlowService(accounts)
	service.fetchLiveModels = func(context.Context, session.StoredAccount) ([]FlowModel, error) {
		return []FlowModel{{ID: "BELUGA", Name: "Nano Banana 2.1"}}, nil
	}
	service.generateLiveImages = func(_ context.Context, _ session.StoredAccount, req GenerateImageRequest) ([]GeneratedImageItem, error) {
		return []GeneratedImageItem{{ID: "img", Prompt: req.Prompt}}, nil
	}
	nodes := map[string]engine.Node{}
	for _, n := range service.Nodes() {
		spec := n.Spec()
		if spec.Provider != service.ID() || !strings.HasPrefix(spec.Type, service.ID()+".") || len(spec.Inputs) == 0 || len(spec.Outputs) == 0 {
			t.Errorf("incomplete spec %+v", spec)
		}
		nodes[spec.Type] = n
	}
	if len(nodes) != 3 || nodes[NodeImageGenerate] == nil || nodes[NodeVideoGenerate] == nil || nodes[NodeVideoEdit] == nil {
		t.Fatalf("unexpected node set %v", nodes)
	}

	out, err := nodes[NodeImageGenerate].Run(context.Background(), json.RawMessage(`{"prompt":"a fox","modelId":"BELUGA","aspectRatio":"1:1","count":1}`))
	if err != nil || out.([]GeneratedImageItem)[0].Prompt != "a fox" {
		t.Fatalf("image node did not run the provider: %+v %v", out, err)
	}
	_, err = nodes[NodeImageGenerate].Run(context.Background(), json.RawMessage(`{"prompt":"a fox"}`))
	if res := engine.Respond[any](nil, err); res.Success || res.Code != models.CodeInvalidArgument {
		t.Fatalf("validation codes must reach the node caller: %+v", res)
	}
}
