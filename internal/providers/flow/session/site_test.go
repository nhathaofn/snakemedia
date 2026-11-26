package session

import (
	"testing"

	"SnakeMedia/internal/accounts"
	"SnakeMedia/internal/config"
	"SnakeMedia/internal/providers/flow/chrome"
)

func TestKeepRPCsMergesOnSameBuildAndResetsOnNewBuild(t *testing.T) {
	stored := chrome.Credentials{
		BuildLabel: "boq_20261126",
		RPCs: chrome.RPCRegistry{
			Models:         "HTrJv",
			Projects:       "UpteDb",
			UploadImage:    "maseQ",
			GenerateImages: "ogiZ0b",
			UpsampleImage:  "SPrCad",
			Credits:        "oldCred",
		},
	}
	// CheckAccount populates Credits and Availability on the same build;
	// previously discovered studio RPCs must survive.
	freshSameBuild := chrome.Credentials{
		BuildLabel: "boq_20261126",
		RPCs: chrome.RPCRegistry{
			Credits:      "nzlxg",
			Availability: "rThb8d",
		},
	}
	got := keepRPCs(freshSameBuild, stored)
	if got.RPCs.Credits != "nzlxg" || got.RPCs.Availability != "rThb8d" {
		t.Fatalf("fresh credits/availability lost: %+v", got.RPCs)
	}
	if got.RPCs.UploadImage != "maseQ" || got.RPCs.GenerateImages != "ogiZ0b" || got.RPCs.UpsampleImage != "SPrCad" {
		t.Fatalf("studio RPCs wiped on same build: %+v", got.RPCs)
	}

	// A new Flow build must NOT inherit stale RPC IDs from the old build.
	freshNewBuild := chrome.Credentials{
		BuildLabel: "boq_20261201",
		RPCs: chrome.RPCRegistry{
			Credits:      "nzlxg",
			Availability: "rThb8d",
		},
	}
	gotNew := keepRPCs(freshNewBuild, stored)
	if gotNew.RPCs.UploadImage != "" || gotNew.RPCs.GenerateImages != "" {
		t.Fatalf("stale RPCs leaked across builds: %+v", gotNew.RPCs)
	}
}

func TestUpdateCredentialsAndBalanceMergeRPCs(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.NewManager(dir + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	accts := accounts.New(dir+"/accounts", cfg, NewSite(cfg))
	rec, err := accts.ImportCookies(accounts.ProviderFlow, "Flow1", "SID=abc").Data, error(nil)
	if rec.ID == "" {
		t.Fatal("failed to seed account")
	}
	svc := NewService(accts, cfg)

	initial := chrome.Credentials{
		AtToken:    "tok-1",
		BuildLabel: "build-A",
		RPCs: chrome.RPCRegistry{
			Credits:        "nzlxg",
			UploadImage:    "maseQ",
			GenerateImages: "ogiZ0b",
			UpsampleImage:  "SPrCad",
		},
	}
	if err := svc.update(rec.ID, func(d *flowData) { d.Credentials = initial }); err != nil {
		t.Fatal(err)
	}

	// Background balance refresh carries only Credits/Availability from an earlier snapshot.
	staleBalanceCreds := chrome.Credentials{
		AtToken:    "tok-1",
		BuildLabel: "build-A",
		RPCs:       chrome.RPCRegistry{Credits: "nzlxg", Availability: "rThb8d"},
	}
	if err := svc.UpdateBalance(rec.ID, chrome.FlowCredits{Credits: 420}, staleBalanceCreds); err != nil {
		t.Fatal(err)
	}
	storedRec, err := accts.Record(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	var data flowData
	if err := accounts.DecodeData(storedRec, &data); err != nil {
		t.Fatal(err)
	}
	if data.Balance == nil || data.Balance.Credits != 420 {
		t.Fatalf("balance not saved: %+v", data.Balance)
	}
	if data.Credentials.RPCs.UploadImage != "maseQ" || data.Credentials.RPCs.UpsampleImage != "SPrCad" || data.Credentials.RPCs.Availability != "rThb8d" {
		t.Fatalf("UpdateBalance clobbered studio RPCs: %+v", data.Credentials.RPCs)
	}
}
